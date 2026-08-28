/*
FILE: internal/ws/conn.go

DESCRIPTION:
Supervised WebSocket connection for the Aster (Binance-style) protocol.
One Conn instance manages one endpoint for its whole lifetime:
  - market mode (combined): connects to <base>/stream, subscribes with
    SUBSCRIBE/UNSUBSCRIBE control frames, dispatches combined-envelope events
    to per-stream handlers;
  - raw mode (user data): connects to a URL produced by the urlFn callback
    (fresh listenKey per connect) and forwards every frame to a single handler.

RELIABILITY:
  - supervise loop: reconnect with exponential backoff + jitter, infinite
    retries until ctx is done;
  - on every (re)connect: per-subscription Reset() callbacks fire BEFORE the
    socket goes live (orderbook engines drop state), then all registered
    streams are re-subscribed in batches;
  - server pings (every ~5 min) are answered with pongs and refresh the read
    deadline; any received frame refreshes it too;
  - the exchange caps client message rate at 10/s — control frames are paced
    by Config.SubscribeInterval and batched by Config.SubscribeBatchSize.

CONCURRENCY:
Thread-safe. gorilla/websocket allows one concurrent writer — all writes are
serialized by writeMu. Handlers are invoked sequentially from the read loop:
a slow handler delays the stream (documented contract, same as sibling SDKs).

DEPENDENCIES:
- github.com/gorilla/websocket: transport.
*/

package ws

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/tonymontanov/go-aster/internal/asterlog"
	"github.com/tonymontanov/go-aster/internal/astermet"
	"github.com/tonymontanov/go-aster/internal/codec"
)

// ErrConnClosed is returned by Subscribe/Unsubscribe after Close.
var ErrConnClosed = errors.New("ws: connection closed")

// Config — WS transport parameters. Populated from the public aster.WsConfig
// by the domain layer (explicit copy to avoid an import cycle).
type Config struct {
	HandshakeTimeout        time.Duration
	ReadTimeout             time.Duration
	WriteTimeout            time.Duration
	ReconnectInitialBackoff time.Duration
	ReconnectMaxBackoff     time.Duration
	ReconnectJitter         float64
	ReadBufferSize          int
	WriteBufferSize         int
	SubscribeBatchSize      int
	SubscribeInterval       time.Duration
}

// Subscription — one stream subscription (market mode).
type Subscription struct {
	// Stream — stream name, e.g. "btcusdt@depth@100ms". Lowercase symbol per
	// the protocol.
	Stream string
	// Handler — receives the raw event payload (already unwrapped from the
	// combined envelope). Called sequentially from the read loop.
	Handler func(payload []byte)
	// Reset — optional; called before every (re)subscribe so that stateful
	// consumers (orderbook engine) drop state accumulated on the previous
	// socket. May be nil.
	Reset func()
}

// Conn — supervised WS connection.
type Conn struct {
	cfg    Config
	urlFn  func(ctx context.Context) (string, error)
	raw    func(payload []byte)
	logger asterlog.Logger

	mu     sync.RWMutex
	subs   map[string]*Subscription
	socket *websocket.Conn
	closed bool

	writeMu   sync.Mutex
	lastCtrl  time.Time
	idSeq     atomic.Uint64
	startOnce sync.Once
	cancel    context.CancelFunc

	cReceived astermet.Counter
	cDropped  astermet.Counter
	cReconn   astermet.Counter
	cSub      astermet.Counter
}

// NewConn creates a market-mode (combined) connection. urlFn returns the full
// WS URL to dial (e.g. "wss://fstream.asterdex.com/stream").
func NewConn(cfg Config, urlFn func(ctx context.Context) (string, error), logger asterlog.Logger, metrics astermet.CounterFactory) *Conn {
	return newConn(cfg, urlFn, nil, logger, metrics)
}

// NewRawConn creates a raw-mode connection: every text frame goes to handler.
// Used for the user-data stream (/ws/<listenKey>); urlFn is called before
// every connect so the dialer always gets a fresh listenKey URL.
func NewRawConn(cfg Config, urlFn func(ctx context.Context) (string, error), handler func(payload []byte), logger asterlog.Logger, metrics astermet.CounterFactory) *Conn {
	return newConn(cfg, urlFn, handler, logger, metrics)
}

func newConn(cfg Config, urlFn func(ctx context.Context) (string, error), raw func(payload []byte), logger asterlog.Logger, metrics astermet.CounterFactory) *Conn {
	if logger == nil {
		logger = asterlog.Noop()
	}
	if metrics == nil {
		metrics = astermet.Noop()
	}
	return &Conn{
		cfg:       cfg,
		urlFn:     urlFn,
		raw:       raw,
		logger:    logger,
		subs:      map[string]*Subscription{},
		cReceived: metrics.Counter("aster_ws_messages_received_total"),
		cDropped:  metrics.Counter("aster_ws_messages_dropped_total"),
		cReconn:   metrics.Counter("aster_ws_reconnects_total"),
		cSub:      metrics.Counter("aster_ws_subscriptions_total"),
	}
}

// Start launches the supervise loop. Idempotent: only the first call has
// effect. The connection lives until ctx is cancelled or Close is called.
func (c *Conn) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		var superviseCtx context.Context
		superviseCtx, c.cancel = context.WithCancel(ctx)
		go c.supervise(superviseCtx)
	})
}

// Close terminates the connection permanently. Safe to call multiple times.
func (c *Conn) Close() {
	c.mu.Lock()
	c.closed = true
	var socket *websocket.Conn = c.socket
	c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if socket != nil {
		_ = socket.Close()
	}
}

// Kick closes the CURRENT socket (if any) without terminating the Conn: the
// supervise loop redials immediately with a fresh URL from urlFn. Used by the
// user-data stream on listenKeyExpired to rotate the listenKey.
func (c *Conn) Kick() {
	c.mu.RLock()
	var socket *websocket.Conn = c.socket
	c.mu.RUnlock()
	if socket != nil {
		_ = socket.Close()
	}
}

/*
Subscribe registers a stream subscription (market mode). Buffered semantics:
the registration is stored first, so subscribing while disconnected is legal —
the stream is subscribed on the next (re)connect. If a socket is live, the
SUBSCRIBE frame is sent immediately.
*/
func (c *Conn) Subscribe(sub *Subscription) error {
	if sub == nil || sub.Stream == "" || sub.Handler == nil {
		return errors.New("ws: invalid subscription")
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrConnClosed
	}
	c.subs[sub.Stream] = sub
	var socket *websocket.Conn = c.socket
	c.mu.Unlock()
	c.cSub.Inc()

	if socket == nil {
		return nil // will subscribe on connect
	}
	return c.sendOp(socket, "SUBSCRIBE", []string{sub.Stream})
}

// Streams returns the names of all registered subscriptions.
func (c *Conn) Streams() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []string = make([]string, 0, len(c.subs))
	var name string
	for name = range c.subs {
		out = append(out, name)
	}
	return out
}

// Unsubscribe removes a stream subscription and, when connected, sends the
// UNSUBSCRIBE frame.
func (c *Conn) Unsubscribe(stream string) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrConnClosed
	}
	delete(c.subs, stream)
	var socket *websocket.Conn = c.socket
	c.mu.Unlock()

	if socket == nil {
		return nil
	}
	return c.sendOp(socket, "UNSUBSCRIBE", []string{stream})
}

// supervise — reconnect loop with backoff + jitter.
func (c *Conn) supervise(ctx context.Context) {
	var backoff time.Duration = c.cfg.ReconnectInitialBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		var err error = c.connectAndRun(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.logger.Warn("ws: connection error, will reconnect", asterlog.Err(err))
		}
		c.cReconn.Inc()

		var sleep time.Duration = applyJitter(backoff, c.cfg.ReconnectJitter)
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
		backoff = nextBackoff(backoff, c.cfg.ReconnectMaxBackoff)
	}
}

// connectAndRun dials, replays subscriptions and runs the read loop until the
// socket dies or ctx is cancelled.
func (c *Conn) connectAndRun(ctx context.Context) error {
	var wsURL string
	var err error
	wsURL, err = c.urlFn(ctx)
	if err != nil {
		return err
	}

	var dialer websocket.Dialer = websocket.Dialer{
		HandshakeTimeout: c.cfg.HandshakeTimeout,
		ReadBufferSize:   c.cfg.ReadBufferSize,
		WriteBufferSize:  c.cfg.WriteBufferSize,
	}
	var socket *websocket.Conn
	socket, _, err = dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}

	// Publish the socket and reset stateful subscribers BEFORE any frame of
	// the new connection is processed, so reconnect cannot mix old and new
	// state (orderbook engines must resync from a fresh snapshot).
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = socket.Close()
		return nil
	}
	c.socket = socket
	var subsCopy []*Subscription = make([]*Subscription, 0, len(c.subs))
	var sub *Subscription
	for _, sub = range c.subs {
		if sub.Reset != nil {
			sub.Reset()
		}
		subsCopy = append(subsCopy, sub)
	}
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.socket = nil
		c.mu.Unlock()
		_ = socket.Close()
	}()

	// Server pings must be answered with a pong carrying the same payload;
	// both pings and pongs refresh the read deadline.
	socket.SetPingHandler(func(payload string) error {
		var deadlineErr error = socket.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))
		if deadlineErr != nil {
			return deadlineErr
		}
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		return socket.WriteControl(websocket.PongMessage, []byte(payload), time.Now().Add(c.cfg.WriteTimeout))
	})
	socket.SetPongHandler(func(string) error {
		return socket.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))
	})

	if err = c.resubscribe(socket, subsCopy); err != nil {
		return err
	}

	// Close the socket when ctx is cancelled so the blocking ReadMessage
	// returns promptly.
	var watchDone chan struct{} = make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = socket.Close()
		case <-watchDone:
		}
	}()

	return c.readLoop(socket)
}

// resubscribe replays all registered streams in batches (market mode only).
func (c *Conn) resubscribe(socket *websocket.Conn, subs []*Subscription) error {
	if c.raw != nil || len(subs) == 0 {
		return nil
	}
	var streams []string = make([]string, 0, len(subs))
	var i int
	for i = 0; i < len(subs); i++ {
		streams = append(streams, subs[i].Stream)
	}

	var batch int = c.cfg.SubscribeBatchSize
	if batch <= 0 {
		batch = 50
	}
	var start int
	for start = 0; start < len(streams); start += batch {
		var end int = start + batch
		if end > len(streams) {
			end = len(streams)
		}
		if err := c.sendOp(socket, "SUBSCRIBE", streams[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// readLoop reads and dispatches frames until the socket dies.
func (c *Conn) readLoop(socket *websocket.Conn) error {
	for {
		var err error = socket.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))
		if err != nil {
			return err
		}
		var msgType int
		var payload []byte
		msgType, payload, err = socket.ReadMessage()
		if err != nil {
			return err
		}
		if msgType != websocket.TextMessage {
			continue
		}
		c.cReceived.Inc()

		if c.raw != nil {
			c.raw(payload)
			continue
		}
		c.dispatch(payload)
	}
}

// dispatch routes one combined-mode frame: control reply or stream event.
func (c *Conn) dispatch(payload []byte) {
	var env combinedEnvelope
	if err := codec.Unmarshal(payload, &env); err != nil || env.Stream == "" {
		// Not a combined event — control reply or noise.
		var reply controlReply
		if err = codec.Unmarshal(payload, &reply); err == nil && reply.ID != 0 {
			if reply.Error != nil {
				c.logger.Warn("ws: control frame rejected",
					asterlog.Int("id", int64(reply.ID)),
					asterlog.Int("code", reply.Error.Code),
					asterlog.Str("msg", reply.Error.Msg))
			}
			return
		}
		c.cDropped.Inc()
		return
	}

	c.mu.RLock()
	var sub *Subscription = c.subs[env.Stream]
	c.mu.RUnlock()
	if sub == nil {
		c.cDropped.Inc()
		return
	}
	sub.Handler(env.Data)
}

/*
sendOp sends one SUBSCRIBE/UNSUBSCRIBE frame. Control frames are paced by
SubscribeInterval (the exchange disconnects clients above 10 msgs/s); pongs
bypass the pacing (they share only writeMu).
*/
func (c *Conn) sendOp(socket *websocket.Conn, method string, streams []string) error {
	var frame opRequest = opRequest{
		Method: method,
		Params: streams,
		ID:     c.idSeq.Add(1),
	}
	var payload []byte
	var err error
	payload, err = codec.Marshal(frame)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var since time.Duration = time.Since(c.lastCtrl)
	if since < c.cfg.SubscribeInterval {
		time.Sleep(c.cfg.SubscribeInterval - since)
	}
	c.lastCtrl = time.Now()

	if err = socket.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout)); err != nil {
		return err
	}
	return socket.WriteMessage(websocket.TextMessage, payload)
}

// applyJitter multiplies d by a random factor in [1-j, 1+j].
func applyJitter(d time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return d
	}
	var factor float64 = 1 + jitter*(2*rand.Float64()-1)
	return time.Duration(float64(d) * factor)
}

// nextBackoff doubles the backoff up to the max.
func nextBackoff(d, maxBackoff time.Duration) time.Duration {
	d *= 2
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}
