/*
FILE: futures/stream-user.go

DESCRIPTION:
User data stream of the Futures profile: listenKey lifecycle + event fan-out.

LISTEN KEY LIFECYCLE (managed automatically — the user never touches it):
  - POST /fapi/v3/listenKey creates (or returns the active) key, valid 60m;
  - the SDK extends it with PUT every Config.WS.ListenKeyKeepaliveInterval
    (default 30m);
  - the stream connects to <ws base>/ws/<listenKey> (raw mode); on every
    reconnect the URL callback POSTs again, so a fresh key is always used;
  - a listenKeyExpired event (or a failed keepalive) kicks the connection,
    which redials with a newly created key.

EVENT FAN-OUT:
One connection serves all subscribers. ORDER_TRADE_UPDATE events go to every
WatchOrderUpdates handler, ACCOUNT_UPDATE to every WatchAccountUpdates
handler. Handlers run sequentially in the read loop. Unknown event types are
logged at debug level and dropped.

LIFETIME:
The ctx of the FIRST Watch* call owns the connection and the keepalive loop.
*/

package futures

import (
	"context"
	"strings"
	"time"

	aster "github.com/tonymontanov/go-aster"
	"github.com/tonymontanov/go-aster/futures/types"
	"github.com/tonymontanov/go-aster/internal/codec"
	"github.com/tonymontanov/go-aster/internal/rest"
	"github.com/tonymontanov/go-aster/internal/ws"
)

// endpointListenKey — user data stream lifecycle endpoint.
const endpointListenKey = "/fapi/v3/listenKey"

/*
WatchOrderUpdates subscribes to order updates (ORDER_TRADE_UPDATE events) of
the account. Requires credentials. The handler receives every order state
transition, including liquidation/ADL orders (see OrderUpdate.ClientOrderID).
*/
func (s *StreamClient) WatchOrderUpdates(ctx context.Context, handler func(types.OrderUpdate), errHandler func(error)) error {
	if handler == nil {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchOrderUpdates: handler is nil", nil)
	}
	if !s.c.signerEnabled() {
		var err error = aster.NewError(aster.ErrorKindAuth, 0, "stream.WatchOrderUpdates: credentials required", nil)
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}

	s.userMu.Lock()
	s.orderHandlers = append(s.orderHandlers, handler)
	if errHandler != nil {
		s.userErrHandlers = append(s.userErrHandlers, errHandler)
	}
	s.userMu.Unlock()

	return s.ensureUserStream(ctx)
}

/*
WatchAccountUpdates subscribes to balance and position updates
(ACCOUNT_UPDATE events) of the account. Requires credentials. Only changed
balances/positions are present in each event.
*/
func (s *StreamClient) WatchAccountUpdates(ctx context.Context, handler func(types.AccountUpdate), errHandler func(error)) error {
	if handler == nil {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "stream.WatchAccountUpdates: handler is nil", nil)
	}
	if !s.c.signerEnabled() {
		var err error = aster.NewError(aster.ErrorKindAuth, 0, "stream.WatchAccountUpdates: credentials required", nil)
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}

	s.userMu.Lock()
	s.accountHandlers = append(s.accountHandlers, handler)
	if errHandler != nil {
		s.userErrHandlers = append(s.userErrHandlers, errHandler)
	}
	s.userMu.Unlock()

	return s.ensureUserStream(ctx)
}

// ensureUserStream lazily creates and starts the user-data connection and
// the listenKey keepalive loop. Idempotent.
func (s *StreamClient) ensureUserStream(ctx context.Context) error {
	s.userMu.Lock()
	defer s.userMu.Unlock()
	if s.userStarted {
		return nil
	}

	var cfg aster.Config = s.c.config()
	var wsBase string = strings.TrimRight(cfg.WS.FuturesURL, "/")
	var urlFn func(context.Context) (string, error) = func(dialCtx context.Context) (string, error) {
		var key string
		var err error
		key, err = s.createListenKey(dialCtx)
		if err != nil {
			return "", err
		}
		return wsBase + "/ws/" + key, nil
	}

	s.user = ws.NewRawConn(toWsConfig(cfg), urlFn, s.dispatchUserEvent, cfg.Logger, cfg.Metrics)
	s.user.Start(ctx)
	go s.keepaliveLoop(ctx, cfg.WS.ListenKeyKeepaliveInterval)
	s.userStarted = true
	return nil
}

// createListenKey — POST /fapi/v3/listenKey. Returns the active listenKey
// (creating one if needed) and extends its validity for 60 minutes.
func (s *StreamClient) createListenKey(ctx context.Context) (string, error) {
	var resp rest.Response
	var err error
	resp, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   endpointListenKey,
		Signed: true,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryQuery)},
	})
	if err != nil {
		return "", err
	}

	var raw struct {
		ListenKey string `json:"listenKey"`
	}
	if err = resp.Unmarshal(&raw); err != nil {
		return "", aster.NewError(aster.ErrorKindUnknown, 0, "stream.createListenKey: parse", err)
	}
	if raw.ListenKey == "" {
		return "", aster.NewError(aster.ErrorKindExchange, 0, "stream.createListenKey: empty listenKey", nil)
	}
	return raw.ListenKey, nil
}

// keepaliveListenKey — PUT /fapi/v3/listenKey. Extends the active key for
// 60 minutes.
func (s *StreamClient) keepaliveListenKey(ctx context.Context) error {
	var err error
	_, _, err = s.c.rest().Do(ctx, rest.Options{
		Method: "PUT",
		Path:   endpointListenKey,
		Signed: true,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryQuery)},
	})
	return err
}

// keepaliveLoop extends the listenKey periodically. A failed keepalive kicks
// the connection: the redial POSTs a fresh key, which also re-arms validity.
func (s *StreamClient) keepaliveLoop(ctx context.Context, interval time.Duration) {
	var renewals = s.c.metrics().Counter("aster_ws_listen_key_renewals_total")
	var ticker *time.Ticker = time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var err error = s.keepaliveListenKey(ctx)
		if err != nil {
			s.c.logger().Warn("stream: listenKey keepalive failed, bouncing user stream", aster.Err(err))
			s.notifyUserError(err)
			s.user.Kick()
			continue
		}
		renewals.Inc()
	}
}

// notifyUserError delivers an error to every registered user-stream error
// handler.
func (s *StreamClient) notifyUserError(err error) {
	s.userMu.RLock()
	var handlers []func(error) = s.userErrHandlers
	s.userMu.RUnlock()
	var i int
	for i = 0; i < len(handlers); i++ {
		handlers[i](err)
	}
}

// dispatchUserEvent routes one user-data frame by event type.
func (s *StreamClient) dispatchUserEvent(payload []byte) {
	var probe struct {
		Event string `json:"e"`
	}
	if err := codec.Unmarshal(payload, &probe); err != nil {
		s.c.logger().Warn("stream: user event parse", aster.Err(err))
		return
	}

	switch probe.Event {
	case "ORDER_TRADE_UPDATE":
		var raw rawOrderUpdateEvent
		if err := codec.Unmarshal(payload, &raw); err != nil {
			s.c.logger().Warn("stream: ORDER_TRADE_UPDATE parse", aster.Err(err))
			return
		}
		var upd types.OrderUpdate = convertOrderUpdate(raw)
		s.userMu.RLock()
		var handlers []func(types.OrderUpdate) = s.orderHandlers
		s.userMu.RUnlock()
		var i int
		for i = 0; i < len(handlers); i++ {
			handlers[i](upd)
		}

	case "ACCOUNT_UPDATE":
		var raw rawAccountUpdateEvent
		if err := codec.Unmarshal(payload, &raw); err != nil {
			s.c.logger().Warn("stream: ACCOUNT_UPDATE parse", aster.Err(err))
			return
		}
		var upd types.AccountUpdate = convertAccountUpdate(raw)
		s.userMu.RLock()
		var handlers []func(types.AccountUpdate) = s.accountHandlers
		s.userMu.RUnlock()
		var i int
		for i = 0; i < len(handlers); i++ {
			handlers[i](upd)
		}

	case "listenKeyExpired":
		// The key died server-side: bounce the socket, the redial creates a
		// fresh key via the URL callback.
		s.c.logger().Warn("stream: listenKey expired, reconnecting user stream")
		s.user.Kick()

	default:
		s.c.logger().Debug("stream: unhandled user event", aster.Str("event", probe.Event))
	}
}

// rawOrderUpdateEvent — wire ORDER_TRADE_UPDATE event.
type rawOrderUpdateEvent struct {
	EventTime int64 `json:"E"`
	TxTime    int64 `json:"T"`
	Order     struct {
		Symbol          string `json:"s"`
		ClientOrderID   string `json:"c"`
		Side            string `json:"S"`
		Type            string `json:"o"`
		TimeInForce     string `json:"f"`
		OrigQty         string `json:"q"`
		Price           string `json:"p"`
		AvgPrice        string `json:"ap"`
		StopPrice       string `json:"sp"`
		ExecutionType   string `json:"x"`
		Status          string `json:"X"`
		OrderID         int64  `json:"i"`
		LastFilledQty   string `json:"l"`
		FilledAccumQty  string `json:"z"`
		LastFilledPrice string `json:"L"`
		CommissionAsset string `json:"N"`
		Commission      string `json:"n"`
		TradeTime       int64  `json:"T"`
		TradeID         int64  `json:"t"`
		BidsNotional    string `json:"b"`
		AsksNotional    string `json:"a"`
		IsMaker         bool   `json:"m"`
		IsReduceOnly    bool   `json:"R"`
		WorkingType     string `json:"wt"`
		OrigType        string `json:"ot"`
		PositionSide    string `json:"ps"`
		IsClosePosition bool   `json:"cp"`
		ActivationPrice string `json:"AP"`
		CallbackRate    string `json:"cr"`
		RealizedProfit  string `json:"rp"`
	} `json:"o"`
}

// convertOrderUpdate maps a rawOrderUpdateEvent into the domain OrderUpdate.
func convertOrderUpdate(raw rawOrderUpdateEvent) types.OrderUpdate {
	var upd types.OrderUpdate = types.OrderUpdate{
		EventTs:         raw.EventTime,
		TxTs:            raw.TxTime,
		Symbol:          raw.Order.Symbol,
		ClientOrderID:   raw.Order.ClientOrderID,
		OrderID:         raw.Order.OrderID,
		Side:            types.SideType(raw.Order.Side),
		PositionSide:    types.PositionSide(raw.Order.PositionSide),
		Type:            types.OrderType(raw.Order.Type),
		OrigType:        types.OrderType(raw.Order.OrigType),
		TimeInForce:     types.TimeInForceType(raw.Order.TimeInForce),
		ExecutionType:   types.ExecutionType(raw.Order.ExecutionType),
		Status:          types.ParseOrderStatus(raw.Order.Status),
		CommissionAsset: raw.Order.CommissionAsset,
		TradeTime:       raw.Order.TradeTime,
		TradeID:         raw.Order.TradeID,
		IsMaker:         raw.Order.IsMaker,
		IsReduceOnly:    raw.Order.IsReduceOnly,
		WorkingType:     types.WorkingType(raw.Order.WorkingType),
		IsClosePosition: raw.Order.IsClosePosition,
	}
	upd.OrigQuantity, _ = codec.ParseDecimal(raw.Order.OrigQty)
	upd.Price, _ = codec.ParseDecimal(raw.Order.Price)
	upd.AvgPrice, _ = codec.ParseDecimal(raw.Order.AvgPrice)
	upd.StopPrice, _ = codec.ParseDecimal(raw.Order.StopPrice)
	upd.LastFilledQuantity, _ = codec.ParseDecimal(raw.Order.LastFilledQty)
	upd.FilledAccumulatedQuantity, _ = codec.ParseDecimal(raw.Order.FilledAccumQty)
	upd.LastFilledPrice, _ = codec.ParseDecimal(raw.Order.LastFilledPrice)
	upd.Commission, _ = codec.ParseDecimal(raw.Order.Commission)
	upd.BidsNotional, _ = codec.ParseDecimal(raw.Order.BidsNotional)
	upd.AsksNotional, _ = codec.ParseDecimal(raw.Order.AsksNotional)
	upd.ActivationPrice, _ = codec.ParseDecimal(raw.Order.ActivationPrice)
	upd.CallbackRate, _ = codec.ParseDecimal(raw.Order.CallbackRate)
	upd.RealizedProfit, _ = codec.ParseDecimal(raw.Order.RealizedProfit)
	return upd
}

// rawAccountUpdateEvent — wire ACCOUNT_UPDATE event.
type rawAccountUpdateEvent struct {
	EventTime int64 `json:"E"`
	TxTime    int64 `json:"T"`
	Data      struct {
		Reason   string `json:"m"`
		Balances []struct {
			Asset              string `json:"a"`
			WalletBalance      string `json:"wb"`
			CrossWalletBalance string `json:"cw"`
			BalanceChange      string `json:"bc"`
		} `json:"B"`
		Positions []struct {
			Symbol              string `json:"s"`
			PositionAmt         string `json:"pa"`
			EntryPrice          string `json:"ep"`
			AccumulatedRealized string `json:"cr"`
			UnrealizedPnl       string `json:"up"`
			MarginType          string `json:"mt"`
			IsolatedWallet      string `json:"iw"`
			PositionSide        string `json:"ps"`
		} `json:"P"`
	} `json:"a"`
}

// convertAccountUpdate maps a rawAccountUpdateEvent into the domain
// AccountUpdate.
func convertAccountUpdate(raw rawAccountUpdateEvent) types.AccountUpdate {
	var upd types.AccountUpdate = types.AccountUpdate{
		EventTs: raw.EventTime,
		TxTs:    raw.TxTime,
		Reason:  types.AccountUpdateReason(raw.Data.Reason),
	}

	var i int
	upd.Balances = make([]types.BalanceUpdate, 0, len(raw.Data.Balances))
	for i = 0; i < len(raw.Data.Balances); i++ {
		var b types.BalanceUpdate = types.BalanceUpdate{Asset: raw.Data.Balances[i].Asset}
		b.WalletBalance, _ = codec.ParseDecimal(raw.Data.Balances[i].WalletBalance)
		b.CrossWalletBalance, _ = codec.ParseDecimal(raw.Data.Balances[i].CrossWalletBalance)
		b.BalanceChange, _ = codec.ParseDecimal(raw.Data.Balances[i].BalanceChange)
		upd.Balances = append(upd.Balances, b)
	}

	upd.Positions = make([]types.PositionUpdate, 0, len(raw.Data.Positions))
	for i = 0; i < len(raw.Data.Positions); i++ {
		var p types.PositionUpdate = types.PositionUpdate{
			Symbol:       raw.Data.Positions[i].Symbol,
			PositionSide: types.PositionSide(raw.Data.Positions[i].PositionSide),
			MarginType:   types.ParseMarginType(raw.Data.Positions[i].MarginType),
		}
		p.PositionAmt, _ = codec.ParseDecimal(raw.Data.Positions[i].PositionAmt)
		p.EntryPrice, _ = codec.ParseDecimal(raw.Data.Positions[i].EntryPrice)
		p.AccumulatedRealized, _ = codec.ParseDecimal(raw.Data.Positions[i].AccumulatedRealized)
		p.UnrealizedPnl, _ = codec.ParseDecimal(raw.Data.Positions[i].UnrealizedPnl)
		p.IsolatedWallet, _ = codec.ParseDecimal(raw.Data.Positions[i].IsolatedWallet)
		upd.Positions = append(upd.Positions, p)
	}
	return upd
}
