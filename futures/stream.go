/*
FILE: futures/stream.go

DESCRIPTION:
Public market-data WS subscriptions of the Futures profile. All Watch*
methods share ONE lazily created market connection (combined /stream endpoint
with dynamic SUBSCRIBE); user-data subscriptions live in stream-user.go on a
separate connection.

CONTRACT (uniform for every Watch*):
  - Watch*(ctx, args..., handler, errHandler) error;
  - the FIRST Watch call's ctx owns the connection lifetime: when it is
    cancelled the socket closes and every subscription of this profile stops;
  - reconnect + resubscribe are transparent; stateful streams (orderbook)
    resync automatically;
  - per-frame parse errors are logged and dropped (not delivered to
    errHandler — a malformed frame storm would flood it); critical errors
    (subscribe rejection, snapshot fetch failure) go to errHandler;
  - handlers are invoked sequentially from the read loop — a slow handler
    delays the stream. Offload heavy work to the caller's goroutines.

ORDERBOOK RESYNC (see orderbook/engine.go for the algorithm):
The engine buffers deltas until a REST snapshot arrives; on any sequence gap
the stream client refetches a snapshot with bounded backoff. Snapshot fetch
runs in a dedicated goroutine — never in the read loop.
*/

package futures

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	aster "github.com/tonymontanov/go-aster"
	"github.com/tonymontanov/go-aster/futures/types"
	"github.com/tonymontanov/go-aster/internal/codec"
	"github.com/tonymontanov/go-aster/internal/ws"
	"github.com/tonymontanov/go-aster/orderbook"
)

// snapshotRetryInitial / snapshotRetryMax — backoff bounds for orderbook
// snapshot refetch after a failure or sequence gap.
const (
	snapshotRetryInitial = 250 * time.Millisecond
	snapshotRetryMax     = 5 * time.Second
)

// StreamClient — WS streams domain client.
type StreamClient struct {
	c *Client

	marketOnce sync.Once
	market     *ws.Conn

	// User-data stream state (stream-user.go).
	userMu          sync.RWMutex
	user            *ws.Conn
	userStarted     bool
	orderHandlers   []func(types.OrderUpdate)
	accountHandlers []func(types.AccountUpdate)
	userErrHandlers []func(error)
}

// newStreamClient — internal constructor.
func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{c: c}
}

// toWsConfig converts the public WsConfig into the internal transport config.
func toWsConfig(cfg aster.Config) ws.Config {
	return ws.Config{
		HandshakeTimeout:        cfg.WS.HandshakeTimeout,
		ReadTimeout:             cfg.WS.ReadTimeout,
		WriteTimeout:            cfg.WS.WriteTimeout,
		ReconnectInitialBackoff: cfg.WS.ReconnectInitialBackoff,
		ReconnectMaxBackoff:     cfg.WS.ReconnectMaxBackoff,
		ReconnectJitter:         cfg.WS.ReconnectJitter,
		ReadBufferSize:          cfg.WS.ReadBufferSize,
		WriteBufferSize:         cfg.WS.WriteBufferSize,
		SubscribeBatchSize:      cfg.WS.SubscribeBatchSize,
		SubscribeInterval:       cfg.WS.SubscribeInterval,
	}
}

// marketConn returns the shared market connection, creating it lazily.
func (s *StreamClient) marketConn() *ws.Conn {
	s.marketOnce.Do(func() {
		var cfg aster.Config = s.c.config()
		var marketURL string = strings.TrimRight(cfg.WS.FuturesURL, "/") + "/stream"
		s.market = ws.NewConn(
			toWsConfig(cfg),
			func(context.Context) (string, error) { return marketURL, nil },
			cfg.Logger,
			cfg.Metrics,
		)
	})
	return s.market
}

// subscribeMarket — shared tail of every market Watch*: start the connection
// on the caller's ctx and register the subscription.
func (s *StreamClient) subscribeMarket(ctx context.Context, sub *ws.Subscription, errHandler func(error)) error {
	s.marketConn().Start(ctx)
	var err error = s.marketConn().Subscribe(sub)
	if err != nil {
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	return nil
}

/*
WatchOrderbook maintains a consistent local order book of the symbol
(diff-depth stream + REST snapshot + automatic resync) and delivers the top
`depth` levels after every applied update.

Parameters:
  - depth: number of levels per side delivered to the handler (<= 0 — full
    book up to Config.Orderbook.MaxDepth);
  - handler: receives a fresh OrderBookSnapshot (slices are NOT reused —
    safe to retain);
  - errHandler: receives snapshot fetch failures and subscribe errors.
*/
func (s *StreamClient) WatchOrderbook(ctx context.Context, symbol string, depth int, handler func(types.OrderBookSnapshot), errHandler func(error)) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchOrderbook: symbol is empty", nil)
	}
	if handler == nil {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchOrderbook: handler is nil", nil)
	}

	var cfg aster.Config = s.c.config()
	var eng *orderbook.Engine = orderbook.NewEngine(symbol, cfg.Orderbook.MaxDepth)
	var resyncing atomic.Bool

	// triggerResync fetches REST snapshots (with backoff) until the engine
	// accepts one, then emits the freshly primed book. One fetcher at a time.
	var triggerResync func()
	triggerResync = func() {
		if !resyncing.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer resyncing.Store(false)
			var backoff time.Duration = snapshotRetryInitial
			for {
				if ctx.Err() != nil {
					return
				}
				var snap types.OrderBookSnapshot
				var err error
				snap, err = s.c.marketData.GetOrderBook(ctx, symbol, cfg.Orderbook.SnapshotLimit)
				if err != nil {
					if errHandler != nil {
						errHandler(err)
					}
				} else {
					var res orderbook.ApplyResult = eng.ApplySnapshot(snap)
					if res.Gap == orderbook.GapNone {
						handler(eng.Snapshot(depth))
						return
					}
					// Replay over this snapshot still gapped — a NEWER
					// snapshot is required; fall through to backoff.
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff *= 2
				if backoff > snapshotRetryMax {
					backoff = snapshotRetryMax
				}
			}
		}()
	}

	var sub *ws.Subscription = &ws.Subscription{
		Stream: strings.ToLower(symbol) + "@depth@100ms",
		Reset: func() {
			// Fresh socket: drop all book state; the first delta of the new
			// connection triggers a snapshot fetch via GapNotPrimed.
			eng.MarkResynced()
		},
		Handler: func(payload []byte) {
			var raw rawDepthUpdate
			if err := codec.Unmarshal(payload, &raw); err != nil {
				s.c.logger().Warn("stream.WatchOrderbook: parse", aster.Str("symbol", symbol), aster.Err(err))
				return
			}
			var delta orderbook.Delta
			var err error
			delta, err = convertDepthUpdate(raw)
			if err != nil {
				s.c.logger().Warn("stream.WatchOrderbook: parse levels", aster.Str("symbol", symbol), aster.Err(err))
				return
			}

			var res orderbook.ApplyResult = eng.ApplyDelta(delta)
			switch res.Gap {
			case orderbook.GapNone:
				if res.Applied {
					handler(eng.Snapshot(depth))
				}
			case orderbook.GapNotPrimed, orderbook.GapSequence:
				triggerResync()
			}
		},
	}
	return s.subscribeMarket(ctx, sub, errHandler)
}

// WatchSpread subscribes to best bid/ask updates (@bookTicker stream).
func (s *StreamClient) WatchSpread(ctx context.Context, symbol string, handler func(types.QuotedSpreadUpdate), errHandler func(error)) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchSpread: symbol is empty", nil)
	}
	if handler == nil {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchSpread: handler is nil", nil)
	}

	var sub *ws.Subscription = &ws.Subscription{
		Stream: strings.ToLower(symbol) + "@bookTicker",
		Handler: func(payload []byte) {
			var raw rawBookTickerEvent
			if err := codec.Unmarshal(payload, &raw); err != nil {
				s.c.logger().Warn("stream.WatchSpread: parse", aster.Str("symbol", symbol), aster.Err(err))
				return
			}
			var upd types.QuotedSpreadUpdate = types.QuotedSpreadUpdate{
				Symbol:   raw.Symbol,
				UpdateID: raw.UpdateID,
				EventTs:  raw.EventTime,
				TxTs:     raw.TxTime,
			}
			upd.BestBidPrice, _ = codec.ParseDecimal(raw.BidPrice)
			upd.BestBidQty, _ = codec.ParseDecimal(raw.BidQty)
			upd.BestAskPrice, _ = codec.ParseDecimal(raw.AskPrice)
			upd.BestAskQty, _ = codec.ParseDecimal(raw.AskQty)
			handler(upd)
		},
	}
	return s.subscribeMarket(ctx, sub, errHandler)
}

// WatchMarkPrice subscribes to mark price / funding updates
// (@markPrice@1s stream).
func (s *StreamClient) WatchMarkPrice(ctx context.Context, symbol string, handler func(types.MarkPriceInfo), errHandler func(error)) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchMarkPrice: symbol is empty", nil)
	}
	if handler == nil {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchMarkPrice: handler is nil", nil)
	}

	var sub *ws.Subscription = &ws.Subscription{
		Stream: strings.ToLower(symbol) + "@markPrice@1s",
		Handler: func(payload []byte) {
			var raw rawMarkPriceEvent
			if err := codec.Unmarshal(payload, &raw); err != nil {
				s.c.logger().Warn("stream.WatchMarkPrice: parse", aster.Str("symbol", symbol), aster.Err(err))
				return
			}
			var upd types.MarkPriceInfo = types.MarkPriceInfo{
				Symbol:          raw.Symbol,
				NextFundingTime: raw.NextFundingTime,
				Ts:              raw.EventTime,
			}
			upd.MarkPrice, _ = codec.ParseDecimal(raw.MarkPrice)
			upd.IndexPrice, _ = codec.ParseDecimal(raw.IndexPrice)
			upd.EstimatedSettlePrice, _ = codec.ParseDecimal(raw.EstimatedSettlePrice)
			upd.LastFundingRate, _ = codec.ParseDecimal(raw.FundingRate)
			handler(upd)
		},
	}
	return s.subscribeMarket(ctx, sub, errHandler)
}

// WatchAggTrades subscribes to aggregated trades (@aggTrade stream). Serves
// as the last-price feed with per-trade granularity.
func (s *StreamClient) WatchAggTrades(ctx context.Context, symbol string, handler func(types.AggTrade), errHandler func(error)) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchAggTrades: symbol is empty", nil)
	}
	if handler == nil {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchAggTrades: handler is nil", nil)
	}

	var sub *ws.Subscription = &ws.Subscription{
		Stream: strings.ToLower(symbol) + "@aggTrade",
		Handler: func(payload []byte) {
			var raw rawAggTradeEvent
			if err := codec.Unmarshal(payload, &raw); err != nil {
				s.c.logger().Warn("stream.WatchAggTrades: parse", aster.Str("symbol", symbol), aster.Err(err))
				return
			}
			var upd types.AggTrade = types.AggTrade{
				Symbol:       raw.Symbol,
				ID:           raw.AggTradeID,
				FirstTradeID: raw.FirstTradeID,
				LastTradeID:  raw.LastTradeID,
				Ts:           raw.TradeTime,
				IsBuyerMaker: raw.IsBuyerMaker,
			}
			upd.Price, _ = codec.ParseDecimal(raw.Price)
			upd.Quantity, _ = codec.ParseDecimal(raw.Quantity)
			handler(upd)
		},
	}
	return s.subscribeMarket(ctx, sub, errHandler)
}

// WatchMiniTicker subscribes to the 24h rolling mini-ticker (@miniTicker
// stream, 1s cadence). The Close field is the latest price.
func (s *StreamClient) WatchMiniTicker(ctx context.Context, symbol string, handler func(types.MiniTicker), errHandler func(error)) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchMiniTicker: symbol is empty", nil)
	}
	if handler == nil {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchMiniTicker: handler is nil", nil)
	}

	var sub *ws.Subscription = &ws.Subscription{
		Stream: strings.ToLower(symbol) + "@miniTicker",
		Handler: func(payload []byte) {
			var raw rawMiniTickerEvent
			if err := codec.Unmarshal(payload, &raw); err != nil {
				s.c.logger().Warn("stream.WatchMiniTicker: parse", aster.Str("symbol", symbol), aster.Err(err))
				return
			}
			var upd types.MiniTicker = types.MiniTicker{
				Symbol:  raw.Symbol,
				EventTs: raw.EventTime,
			}
			upd.Close, _ = codec.ParseDecimal(raw.Close)
			upd.Open, _ = codec.ParseDecimal(raw.Open)
			upd.High, _ = codec.ParseDecimal(raw.High)
			upd.Low, _ = codec.ParseDecimal(raw.Low)
			upd.Volume, _ = codec.ParseDecimal(raw.Volume)
			upd.QuoteVolume, _ = codec.ParseDecimal(raw.QuoteVolume)
			handler(upd)
		},
	}
	return s.subscribeMarket(ctx, sub, errHandler)
}

// WatchKline subscribes to kline updates (@kline_<interval> stream).
func (s *StreamClient) WatchKline(ctx context.Context, symbol string, timeframe types.Timeframe, handler func(types.KlineUpdate), errHandler func(error)) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchKline: symbol is empty", nil)
	}
	if !types.ValidateTimeframe(timeframe) {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchKline: invalid timeframe "+string(timeframe), nil)
	}
	if handler == nil {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchKline: handler is nil", nil)
	}

	var sub *ws.Subscription = &ws.Subscription{
		Stream: strings.ToLower(symbol) + "@kline_" + string(timeframe),
		Handler: func(payload []byte) {
			var raw rawKlineEvent
			if err := codec.Unmarshal(payload, &raw); err != nil {
				s.c.logger().Warn("stream.WatchKline: parse", aster.Str("symbol", symbol), aster.Err(err))
				return
			}
			var upd types.KlineUpdate = types.KlineUpdate{
				Symbol:    raw.Symbol,
				Timeframe: types.Timeframe(raw.Kline.Interval),
				IsClosed:  raw.Kline.IsClosed,
				EventTs:   raw.EventTime,
			}
			upd.Candle.OpenTime = raw.Kline.OpenTime
			upd.Candle.CloseTime = raw.Kline.CloseTime
			upd.Candle.Trades = raw.Kline.Trades
			upd.Candle.Open, _ = codec.ParseDecimal(raw.Kline.Open)
			upd.Candle.High, _ = codec.ParseDecimal(raw.Kline.High)
			upd.Candle.Low, _ = codec.ParseDecimal(raw.Kline.Low)
			upd.Candle.Close, _ = codec.ParseDecimal(raw.Kline.Close)
			upd.Candle.Volume, _ = codec.ParseDecimal(raw.Kline.Volume)
			upd.Candle.QuoteVolume, _ = codec.ParseDecimal(raw.Kline.QuoteVolume)
			upd.Candle.TakerBuyBaseVolume, _ = codec.ParseDecimal(raw.Kline.TakerBuyBase)
			upd.Candle.TakerBuyQuoteVolume, _ = codec.ParseDecimal(raw.Kline.TakerBuyQuote)
			handler(upd)
		},
	}
	return s.subscribeMarket(ctx, sub, errHandler)
}

/*
UnwatchSymbol removes ALL market subscriptions of the symbol from the shared
connection (used for hot symbol switching). Idempotent: unknown symbols are a
no-op. User-data subscriptions are account-scoped and unaffected.
*/
func (s *StreamClient) UnwatchSymbol(symbol string) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.UnwatchSymbol: symbol is empty", nil)
	}
	var conn *ws.Conn = s.marketConn()
	var prefix string = strings.ToLower(symbol) + "@"
	var streams []string = conn.Streams()
	var i int
	for i = 0; i < len(streams); i++ {
		if strings.HasPrefix(streams[i], prefix) {
			if err := conn.Unsubscribe(streams[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// rawDepthUpdate — wire @depth event.
type rawDepthUpdate struct {
	EventTime     int64       `json:"E"`
	TxTime        int64       `json:"T"`
	Symbol        string      `json:"s"`
	FirstUpdateID int64       `json:"U"`
	FinalUpdateID int64       `json:"u"`
	PrevFinalID   int64       `json:"pu"`
	Bids          [][2]string `json:"b"`
	Asks          [][2]string `json:"a"`
}

// convertDepthUpdate maps a rawDepthUpdate into an orderbook.Delta.
func convertDepthUpdate(raw rawDepthUpdate) (orderbook.Delta, error) {
	var delta orderbook.Delta = orderbook.Delta{
		FirstUpdateID:     raw.FirstUpdateID,
		FinalUpdateID:     raw.FinalUpdateID,
		PrevFinalUpdateID: raw.PrevFinalID,
		EventTs:           raw.EventTime,
		TxTs:              raw.TxTime,
	}
	var err error
	delta.Bids, err = parseBookLevels(raw.Bids)
	if err != nil {
		return delta, err
	}
	delta.Asks, err = parseBookLevels(raw.Asks)
	return delta, err
}

// rawBookTickerEvent — wire @bookTicker event.
type rawBookTickerEvent struct {
	UpdateID  int64  `json:"u"`
	EventTime int64  `json:"E"`
	TxTime    int64  `json:"T"`
	Symbol    string `json:"s"`
	BidPrice  string `json:"b"`
	BidQty    string `json:"B"`
	AskPrice  string `json:"a"`
	AskQty    string `json:"A"`
}

// rawMarkPriceEvent — wire @markPrice event.
type rawMarkPriceEvent struct {
	EventTime            int64  `json:"E"`
	Symbol               string `json:"s"`
	MarkPrice            string `json:"p"`
	IndexPrice           string `json:"i"`
	EstimatedSettlePrice string `json:"P"`
	FundingRate          string `json:"r"`
	NextFundingTime      int64  `json:"T"`
}

// rawAggTradeEvent — wire @aggTrade event.
type rawAggTradeEvent struct {
	EventTime    int64  `json:"E"`
	Symbol       string `json:"s"`
	AggTradeID   int64  `json:"a"`
	Price        string `json:"p"`
	Quantity     string `json:"q"`
	FirstTradeID int64  `json:"f"`
	LastTradeID  int64  `json:"l"`
	TradeTime    int64  `json:"T"`
	IsBuyerMaker bool   `json:"m"`
}

// rawMiniTickerEvent — wire @miniTicker event.
type rawMiniTickerEvent struct {
	EventTime   int64  `json:"E"`
	Symbol      string `json:"s"`
	Close       string `json:"c"`
	Open        string `json:"o"`
	High        string `json:"h"`
	Low         string `json:"l"`
	Volume      string `json:"v"`
	QuoteVolume string `json:"q"`
}

// rawKlineEvent — wire @kline event.
type rawKlineEvent struct {
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`
	Kline     struct {
		OpenTime      int64  `json:"t"`
		CloseTime     int64  `json:"T"`
		Interval      string `json:"i"`
		Open          string `json:"o"`
		Close         string `json:"c"`
		High          string `json:"h"`
		Low           string `json:"l"`
		Volume        string `json:"v"`
		Trades        int64  `json:"n"`
		IsClosed      bool   `json:"x"`
		QuoteVolume   string `json:"q"`
		TakerBuyBase  string `json:"V"`
		TakerBuyQuote string `json:"Q"`
	} `json:"k"`
}
