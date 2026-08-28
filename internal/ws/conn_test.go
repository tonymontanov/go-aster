/*
FILE: internal/ws/conn_test.go

DESCRIPTION:
Tests of the WS supervisor against a mock websocket server: subscribe frame
format, combined-envelope dispatch, reconnect with resubscribe + Reset,
raw mode, server ping handling and Kick.
*/

package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/tonymontanov/go-aster/internal/codec"
)

// testWsServer — mock websocket endpoint recording client frames.
type testWsServer struct {
	t        *testing.T
	srv      *httptest.Server
	upgrader websocket.Upgrader

	mu       sync.Mutex
	conns    []*websocket.Conn
	frames   []string
	connSeen chan *websocket.Conn
}

func newTestWsServer(t *testing.T) *testWsServer {
	var s *testWsServer = &testWsServer{t: t, connSeen: make(chan *websocket.Conn, 8)}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var conn *websocket.Conn
		var err error
		conn, err = s.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		s.connSeen <- conn
		go func() {
			for {
				var _, payload, readErr = conn.ReadMessage()
				if readErr != nil {
					return
				}
				s.mu.Lock()
				s.frames = append(s.frames, string(payload))
				s.mu.Unlock()
			}
		}()
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *testWsServer) url() string {
	return "ws" + strings.TrimPrefix(s.srv.URL, "http")
}

func (s *testWsServer) waitConn(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case conn := <-s.connSeen:
		return conn
	case <-time.After(3 * time.Second):
		t.Fatal("no ws connection established")
		return nil
	}
}

func (s *testWsServer) sentFrames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string = make([]string, len(s.frames))
	copy(out, s.frames)
	return out
}

func (s *testWsServer) waitFrameContaining(t *testing.T, substr string) string {
	t.Helper()
	var deadline time.Time = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var frames []string = s.sentFrames()
		var i int
		for i = 0; i < len(frames); i++ {
			if strings.Contains(frames[i], substr) {
				return frames[i]
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no client frame containing %q; got %v", substr, s.sentFrames())
	return ""
}

func testConfig() Config {
	return Config{
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             5 * time.Second,
		WriteTimeout:            2 * time.Second,
		ReconnectInitialBackoff: 20 * time.Millisecond,
		ReconnectMaxBackoff:     100 * time.Millisecond,
		ReconnectJitter:         0.1,
		SubscribeBatchSize:      50,
		SubscribeInterval:       time.Millisecond,
	}
}

func TestSubscribeDispatchAndReconnect(t *testing.T) {
	var server *testWsServer = newTestWsServer(t)
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var conn *Conn = NewConn(testConfig(), func(context.Context) (string, error) { return server.url(), nil }, nil, nil)

	var mu sync.Mutex
	var received []string
	var resets int
	var sub *Subscription = &Subscription{
		Stream: "btcusdt@depth@100ms",
		Handler: func(payload []byte) {
			mu.Lock()
			received = append(received, string(payload))
			mu.Unlock()
		},
		Reset: func() {
			mu.Lock()
			resets++
			mu.Unlock()
		},
	}
	// Subscribe BEFORE start: must be buffered and sent on connect.
	if err := conn.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	conn.Start(ctx)

	var serverConn *websocket.Conn = server.waitConn(t)
	var frame string = server.waitFrameContaining(t, "SUBSCRIBE")
	var op opRequest
	if err := codec.Unmarshal([]byte(frame), &op); err != nil {
		t.Fatalf("subscribe frame not parseable: %v", err)
	}
	if op.Method != "SUBSCRIBE" || len(op.Params) != 1 || op.Params[0] != "btcusdt@depth@100ms" || op.ID == 0 {
		t.Fatalf("subscribe frame wrong: %+v", op)
	}

	// Combined event → dispatched to the handler; ack frame → ignored.
	_ = serverConn.WriteMessage(websocket.TextMessage, []byte(`{"result":null,"id":1}`))
	_ = serverConn.WriteMessage(websocket.TextMessage, []byte(`{"stream":"btcusdt@depth@100ms","data":{"e":"depthUpdate","u":7}}`))
	var deadline time.Time = time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		var n int = len(received)
		mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("event not dispatched; received=%d", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	if !strings.Contains(received[0], `"u":7`) {
		t.Fatalf("payload not unwrapped: %s", received[0])
	}
	var resetsAfterFirstConnect int = resets
	mu.Unlock()
	if resetsAfterFirstConnect != 1 {
		t.Fatalf("Reset on first connect = %d, want 1", resetsAfterFirstConnect)
	}

	// Kill the socket server-side: the client must reconnect, fire Reset and
	// resubscribe.
	_ = serverConn.Close()
	_ = server.waitConn(t)
	server.mu.Lock()
	server.frames = nil
	server.mu.Unlock()
	server.waitFrameContaining(t, "SUBSCRIBE")
	mu.Lock()
	var resetsAfterReconnect int = resets
	mu.Unlock()
	if resetsAfterReconnect != 2 {
		t.Fatalf("Reset on reconnect = %d, want 2", resetsAfterReconnect)
	}
}

func TestUnsubscribeSendsFrame(t *testing.T) {
	var server *testWsServer = newTestWsServer(t)
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var conn *Conn = NewConn(testConfig(), func(context.Context) (string, error) { return server.url(), nil }, nil, nil)
	_ = conn.Subscribe(&Subscription{Stream: "btcusdt@bookTicker", Handler: func([]byte) {}})
	conn.Start(ctx)
	_ = server.waitConn(t)
	server.waitFrameContaining(t, "SUBSCRIBE")

	if err := conn.Unsubscribe("btcusdt@bookTicker"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	server.waitFrameContaining(t, "UNSUBSCRIBE")
	if len(conn.Streams()) != 0 {
		t.Fatalf("stream registry not cleaned: %v", conn.Streams())
	}
}

func TestRawModeAndKick(t *testing.T) {
	var server *testWsServer = newTestWsServer(t)
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var received []string
	var urlCalls int
	var conn *Conn = NewRawConn(testConfig(), func(context.Context) (string, error) {
		mu.Lock()
		urlCalls++
		mu.Unlock()
		return server.url(), nil
	}, func(payload []byte) {
		mu.Lock()
		received = append(received, string(payload))
		mu.Unlock()
	}, nil, nil)
	conn.Start(ctx)

	var serverConn *websocket.Conn = server.waitConn(t)
	_ = serverConn.WriteMessage(websocket.TextMessage, []byte(`{"e":"ACCOUNT_UPDATE"}`))
	var deadline time.Time = time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		var n int = len(received)
		mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("raw frame not delivered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Kick must force a redial → urlFn called again (fresh listenKey path).
	conn.Kick()
	_ = server.waitConn(t)
	mu.Lock()
	var calls int = urlCalls
	mu.Unlock()
	if calls < 2 {
		t.Fatalf("urlFn calls = %d, want >= 2 after Kick", calls)
	}
}

func TestServerPingAnsweredWithPong(t *testing.T) {
	var server *testWsServer = newTestWsServer(t)
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var conn *Conn = NewConn(testConfig(), func(context.Context) (string, error) { return server.url(), nil }, nil, nil)
	conn.Start(ctx)
	var serverConn *websocket.Conn = server.waitConn(t)

	var pongCh chan string = make(chan string, 1)
	serverConn.SetPongHandler(func(payload string) error {
		select {
		case pongCh <- payload:
		default:
		}
		return nil
	})
	// The server read loop must be active for control handlers to fire; the
	// mock's reader goroutine already runs. Send a ping and await the pong.
	_ = serverConn.WriteControl(websocket.PingMessage, []byte("hb"), time.Now().Add(time.Second))

	select {
	case payload := <-pongCh:
		if payload != "hb" {
			t.Fatalf("pong payload = %q, want %q", payload, "hb")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no pong received")
	}
}
