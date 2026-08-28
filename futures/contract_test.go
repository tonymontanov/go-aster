/*
FILE: futures/contract_test.go

DESCRIPTION:
Contract tests of the futures domain clients against an httptest mock serving
JSON fixtures taken from the official Aster V3 API documentation. If the
exchange contract changes shape, these tests fail first.

The mock routes by "<METHOD> <path>"; unknown routes return 404 so a
misrouted request fails loudly.
*/

package futures_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/shopspring/decimal"

	aster "github.com/tonymontanov/go-aster"
	"github.com/tonymontanov/go-aster/futures"
	"github.com/tonymontanov/go-aster/futures/types"
)

// Public demonstration credentials from the Aster V3 docs.
const (
	testPrivateKey = "0x4fd0a42218f3eae43a6ce26d22544e986139a01e5b34a62db53757ffca81bae1"
	testUser       = "0x63DD5aCC6b1aa0f563956C0e534DD30B6dcF7C4e"
)

// capturedRequest — last request seen by the mock for a route.
type capturedRequest struct {
	Params url.Values
	Hits   int64
}

// mockAster serves fixtures by "<METHOD> <path>" and captures request params.
func mockAster(t *testing.T, routes map[string]string) (*httptest.Server, map[string]*capturedRequest) {
	t.Helper()
	var captures map[string]*capturedRequest = map[string]*capturedRequest{}
	var key string
	for key = range routes {
		captures[key] = &capturedRequest{}
	}

	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var routeKey string = r.Method + " " + r.URL.Path
		var fixture string
		var ok bool
		fixture, ok = routes[routeKey]
		if !ok {
			t.Errorf("unexpected request: %s", routeKey)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":-1121,"msg":"unexpected route"}`))
			return
		}

		var cap *capturedRequest = captures[routeKey]
		atomic.AddInt64(&cap.Hits, 1)
		var raw []byte
		raw, _ = io.ReadAll(r.Body)
		if len(raw) > 0 {
			cap.Params, _ = url.ParseQuery(string(raw))
		} else {
			cap.Params = r.URL.Query()
		}
		_, _ = w.Write([]byte(fixture))
	}))
	t.Cleanup(srv.Close)
	return srv, captures
}

// newTestFutures builds a futures client over the mock server.
func newTestFutures(t *testing.T, srv *httptest.Server) *futures.Client {
	t.Helper()
	var client *aster.Client
	var err error
	client, err = aster.NewClient(aster.Config{
		User:       testUser,
		PrivateKey: testPrivateKey,
		REST:       aster.RestConfig{FuturesBaseURL: srv.URL},
		WS:         aster.WsConfig{FuturesURL: "ws://127.0.0.1:1"},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var f *futures.Client
	var ok bool
	f, ok = client.Futures().(*futures.Client)
	if !ok || f == nil {
		t.Fatal("Futures() did not return *futures.Client")
	}
	return f
}

// createOrderFixture — POST /fapi/v3/order response from the docs.
const createOrderFixture = `{
	"clientOrderId": "testOrder", "cumQty": "0", "cumQuote": "0",
	"executedQty": "0", "orderId": 22542179, "avgPrice": "0.00000",
	"origQty": "10", "price": "0.5", "reduceOnly": false, "side": "BUY",
	"positionSide": "BOTH", "status": "NEW", "stopPrice": "0",
	"closePosition": false, "symbol": "BTCUSDT", "timeInForce": "GTC",
	"type": "LIMIT", "origType": "LIMIT", "updateTime": 1566818724722,
	"workingType": "CONTRACT_PRICE", "priceProtect": false
}`

func TestCreateOrder(t *testing.T) {
	var srv, captures = mockAster(t, map[string]string{
		"POST /fapi/v3/order": createOrderFixture,
	})
	var f *futures.Client = newTestFutures(t, srv)

	var req types.CreateOrderRequest = types.CreateOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          types.SideTypeBuy,
		Type:          types.OrderTypeLimit,
		TimeInForce:   types.TimeInForceTypeGTC,
		Quantity:      decimal.RequireFromString("10"),
		Price:         decimal.RequireFromString("0.5"),
		ClientOrderID: "testOrder",
	}
	var info types.OrderInfo
	var err error
	info, err = f.Trading().CreateOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	// Wire params.
	var sent url.Values = captures["POST /fapi/v3/order"].Params
	if sent.Get("symbol") != "BTCUSDT" || sent.Get("side") != "BUY" ||
		sent.Get("type") != "LIMIT" || sent.Get("timeInForce") != "GTC" ||
		sent.Get("quantity") != "10" || sent.Get("price") != "0.5" ||
		sent.Get("newClientOrderId") != "testOrder" {
		t.Fatalf("wire params wrong: %v", sent)
	}
	if sent.Get("signature") == "" || sent.Get("nonce") == "" || sent.Get("signer") == "" {
		t.Fatalf("auth params missing: %v", sent)
	}

	// Parsed response.
	if info.OrderID != 22542179 || info.ClientOrderID != "testOrder" {
		t.Fatalf("ids wrong: %+v", info)
	}
	if info.Status != types.OrderStatusNew || info.Side != types.SideTypeBuy {
		t.Fatalf("enums wrong: %+v", info)
	}
	if !info.Price.Equal(decimal.RequireFromString("0.5")) || !info.OrigQuantity.Equal(decimal.RequireFromString("10")) {
		t.Fatalf("decimals wrong: %+v", info)
	}
	if info.CreatedTime != 1566818724722 {
		t.Fatalf("CreatedTime must anchor on updateTime for acks: %+v", info)
	}

	// Mapping cache.
	var orderID int64
	var ok bool
	orderID, ok = f.Trading().OrderIDByClientID("testOrder")
	if !ok || orderID != 22542179 {
		t.Fatalf("id mapping not cached: %d %v", orderID, ok)
	}
}

func TestCreateOrderLocalValidation(t *testing.T) {
	var srv, captures = mockAster(t, map[string]string{
		"POST /fapi/v3/order": createOrderFixture,
	})
	var f *futures.Client = newTestFutures(t, srv)

	// LIMIT without TimeInForce must fail locally, without a network call.
	var req types.CreateOrderRequest = types.CreateOrderRequest{
		Symbol:   "BTCUSDT",
		Side:     types.SideTypeBuy,
		Type:     types.OrderTypeLimit,
		Quantity: decimal.RequireFromString("10"),
		Price:    decimal.RequireFromString("0.5"),
	}
	var err error
	_, err = f.Trading().CreateOrder(context.Background(), req)
	if !aster.IsInvalidRequest(err) {
		t.Fatalf("expected local validation error, got %v", err)
	}
	if captures["POST /fapi/v3/order"].Hits != 0 {
		t.Fatal("validation error must not hit the network")
	}
}

func TestCreateBatchOrdersPartialSuccess(t *testing.T) {
	var srv, captures = mockAster(t, map[string]string{
		"POST /fapi/v3/batchOrders": `[` + createOrderFixture + `,{"code":-2022,"msg":"ReduceOnly Order is rejected."}]`,
	})
	var f *futures.Client = newTestFutures(t, srv)

	var reqs []types.CreateOrderRequest = []types.CreateOrderRequest{
		{
			Symbol: "BTCUSDT", Side: types.SideTypeBuy, Type: types.OrderTypeLimit,
			TimeInForce: types.TimeInForceTypeGTC,
			Quantity:    decimal.RequireFromString("10"), Price: decimal.RequireFromString("0.5"),
		},
		{
			Symbol: "BTCUSDT", Side: types.SideTypeSell, Type: types.OrderTypeLimit,
			TimeInForce: types.TimeInForceTypeGTC,
			Quantity:    decimal.RequireFromString("5"), Price: decimal.RequireFromString("0.6"),
		},
	}
	var infos []types.OrderInfo
	var err error
	infos, err = f.Trading().CreateBatchOrders(context.Background(), reqs)

	if len(infos) != 1 {
		t.Fatalf("accepted rows = %d, want 1", len(infos))
	}
	if err == nil {
		t.Fatal("expected joined row error")
	}
	var apiErr *aster.Error
	if !errors.As(err, &apiErr) || apiErr.AsterCode != -2022 {
		t.Fatalf("row error lost: %v", err)
	}
	if captures["POST /fapi/v3/batchOrders"].Params.Get("batchOrders") == "" {
		t.Fatal("batchOrders param missing")
	}
}

func TestCancelOrderAndBatchCancel(t *testing.T) {
	var cancelFixture string = `{
		"clientOrderId": "myOrder1", "orderId": 283194212, "symbol": "BTCUSDT",
		"status": "CANCELED", "side": "BUY", "origQty": "11", "price": "0",
		"type": "LIMIT", "origType": "LIMIT", "timeInForce": "GTC",
		"updateTime": 1571110484038
	}`
	var srv, captures = mockAster(t, map[string]string{
		"DELETE /fapi/v3/order":       cancelFixture,
		"DELETE /fapi/v3/batchOrders": `[` + cancelFixture + `,{"code":-2011,"msg":"Unknown order sent."}]`,
	})
	var f *futures.Client = newTestFutures(t, srv)

	var err error
	err = f.Trading().CancelOrder(context.Background(), types.CancelOrderRequest{Symbol: "BTCUSDT", OrderID: 283194212})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if captures["DELETE /fapi/v3/order"].Params.Get("orderId") != "283194212" {
		t.Fatalf("cancel params wrong: %v", captures["DELETE /fapi/v3/order"].Params)
	}

	err = f.Trading().CancelBatchOrders(context.Background(), []types.CancelOrderRequest{
		{Symbol: "BTCUSDT", OrderID: 283194212},
		{Symbol: "BTCUSDT", OrderID: 99},
	})
	if err == nil {
		t.Fatal("expected joined row error from batch cancel")
	}
	var sent url.Values = captures["DELETE /fapi/v3/batchOrders"].Params
	if sent.Get("orderIdList") != "[283194212,99]" {
		t.Fatalf("orderIdList format wrong: %q", sent.Get("orderIdList"))
	}
}

func TestGetOpenOrdersRefreshesMappings(t *testing.T) {
	var srv, _ = mockAster(t, map[string]string{
		"GET /fapi/v3/openOrders": `[{
			"symbol": "BTCUSDT", "orderId": 1917641, "clientOrderId": "abc",
			"side": "BUY", "positionSide": "BOTH", "status": "NEW",
			"price": "0.1", "avgPrice": "0.0", "origQty": "0.4",
			"executedQty": "0", "cumQuote": "0", "type": "LIMIT",
			"origType": "LIMIT", "timeInForce": "GTC",
			"time": 1579276756075, "updateTime": 1579276756075
		}]`,
	})
	var f *futures.Client = newTestFutures(t, srv)

	var infos []types.OrderInfo
	var err error
	infos, err = f.Trading().GetOpenOrders(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	if len(infos) != 1 || infos[0].CreatedTime != 1579276756075 {
		t.Fatalf("parse wrong: %+v", infos)
	}
	var clOrd string
	var ok bool
	clOrd, ok = f.Trading().ClientIDByOrderID(1917641)
	if !ok || clOrd != "abc" {
		t.Fatal("mapping not refreshed from open orders")
	}
}

func TestGetPositions(t *testing.T) {
	var srv, _ = mockAster(t, map[string]string{
		"GET /fapi/v3/positionRisk": `[{
			"entryPrice": "6563.66500", "marginType": "isolated",
			"isAutoAddMargin": "false", "isolatedMargin": "15517.54150468",
			"leverage": "10", "liquidationPrice": "5930.78",
			"markPrice": "6679.50671178", "maxNotionalValue": "20000000",
			"positionAmt": "20.000", "symbol": "BTCUSDT",
			"unRealizedProfit": "2316.83423560", "positionSide": "LONG",
			"updateTime": 1625474304765
		}]`,
	})
	var f *futures.Client = newTestFutures(t, srv)

	var positions []types.PositionInfo
	var err error
	positions, err = f.Account().GetPositions(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	var p types.PositionInfo = positions[0]
	if p.PositionSide != "LONG" || p.Leverage != 10 || p.MarginType != types.MarginTypeIsolated {
		t.Fatalf("enums wrong: %+v", p)
	}
	if !p.PositionAmt.Equal(decimal.RequireFromString("20.000")) {
		t.Fatalf("amount wrong: %s", p.PositionAmt)
	}
	if !p.EntryPrice.Equal(decimal.RequireFromString("6563.665")) {
		t.Fatalf("entry price wrong: %s", p.EntryPrice)
	}
}

func TestGetBalances(t *testing.T) {
	var srv, _ = mockAster(t, map[string]string{
		"GET /fapi/v3/balance": `[{
			"accountAlias": "SgsR", "asset": "USDT",
			"balance": "122607.35137903", "crossWalletBalance": "23.72469206",
			"crossUnPnl": "0.00000000", "availableBalance": "23.72469206",
			"maxWithdrawAmount": "23.72469206", "marginAvailable": true,
			"updateTime": 1617939110373
		}]`,
	})
	var f *futures.Client = newTestFutures(t, srv)

	var balances []types.Balance
	var err error
	balances, err = f.Account().GetBalances(context.Background())
	if err != nil {
		t.Fatalf("GetBalances: %v", err)
	}
	if len(balances) != 1 || balances[0].Asset != "USDT" {
		t.Fatalf("parse wrong: %+v", balances)
	}
	if !balances[0].WalletBalance.Equal(decimal.RequireFromString("122607.35137903")) {
		t.Fatalf("balance wrong: %s", balances[0].WalletBalance)
	}
}

// exchangeInfoFixture — trimmed docs response with one symbol.
const exchangeInfoFixture = `{
	"timezone": "UTC", "serverTime": 1565613908500,
	"rateLimits": [
		{"interval": "MINUTE", "intervalNum": 1, "limit": 2400, "rateLimitType": "REQUEST_WEIGHT"},
		{"interval": "MINUTE", "intervalNum": 1, "limit": 1200, "rateLimitType": "ORDERS"}
	],
	"assets": [{"asset": "USDT", "marginAvailable": true, "autoAssetExchange": 0}],
	"symbols": [{
		"symbol": "BTCUSDT", "pair": "BTCUSDT", "contractType": "PERPETUAL",
		"status": "TRADING", "baseAsset": "BTC", "quoteAsset": "USDT",
		"marginAsset": "USDT", "pricePrecision": 5, "quantityPrecision": 3,
		"triggerProtect": "0.15", "liquidationFee": "0.010000",
		"marketTakeBound": "0.30",
		"filters": [
			{"filterType": "PRICE_FILTER", "maxPrice": "300", "minPrice": "0.0001", "tickSize": "0.0001"},
			{"filterType": "LOT_SIZE", "maxQty": "10000000", "minQty": "1", "stepSize": "1"},
			{"filterType": "MARKET_LOT_SIZE", "maxQty": "590119", "minQty": "1", "stepSize": "1"},
			{"filterType": "MAX_NUM_ORDERS", "limit": 200},
			{"filterType": "MIN_NOTIONAL", "notional": "1"},
			{"filterType": "PERCENT_PRICE", "multiplierUp": "1.1500", "multiplierDown": "0.8500", "multiplierDecimal": 4}
		],
		"OrderType": ["LIMIT", "MARKET", "STOP"],
		"timeInForce": ["GTC", "IOC", "FOK", "GTX", "HIDDEN"]
	}]
}`

func TestGetSymbolInfoFlattensFiltersAndCaches(t *testing.T) {
	var srv, captures = mockAster(t, map[string]string{
		"GET /fapi/v3/exchangeInfo": exchangeInfoFixture,
	})
	var f *futures.Client = newTestFutures(t, srv)

	var info types.SymbolInfo
	var err error
	info, err = f.MarketData().GetSymbolInfo(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("GetSymbolInfo: %v", err)
	}
	if !info.TickSize.Equal(decimal.RequireFromString("0.0001")) ||
		!info.StepSize.Equal(decimal.RequireFromString("1")) ||
		!info.MinNotional.Equal(decimal.RequireFromString("1")) ||
		info.MaxNumOrders != 200 {
		t.Fatalf("filters not flattened: %+v", info)
	}
	if len(info.TimeInForce) != 5 || info.TimeInForce[4] != types.TimeInForceTypeHidden {
		t.Fatalf("timeInForce wrong: %v", info.TimeInForce)
	}

	// Second lookup must come from the cache (single upstream hit).
	_, err = f.MarketData().GetSymbolInfo(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("cached GetSymbolInfo: %v", err)
	}
	if captures["GET /fapi/v3/exchangeInfo"].Hits != 1 {
		t.Fatalf("exchangeInfo hits = %d, want 1 (cache)", captures["GET /fapi/v3/exchangeInfo"].Hits)
	}
}

func TestGetOrderBook(t *testing.T) {
	var srv, captures = mockAster(t, map[string]string{
		"GET /fapi/v3/depth": `{
			"lastUpdateId": 1027024, "E": 1589436922972, "T": 1589436922959,
			"bids": [["4.00000000", "431.00000000"]],
			"asks": [["4.00000200", "12.00000000"]]
		}`,
	})
	var f *futures.Client = newTestFutures(t, srv)

	var snap types.OrderBookSnapshot
	var err error
	snap, err = f.MarketData().GetOrderBook(context.Background(), "BTCUSDT", 30)
	if err != nil {
		t.Fatalf("GetOrderBook: %v", err)
	}
	// 30 is not an allowed exchange limit — must clamp UP to 50.
	if captures["GET /fapi/v3/depth"].Params.Get("limit") != "50" {
		t.Fatalf("limit not clamped: %v", captures["GET /fapi/v3/depth"].Params)
	}
	if snap.LastUpdateID != 1027024 || len(snap.Bids) != 1 || len(snap.Asks) != 1 {
		t.Fatalf("snapshot wrong: %+v", snap)
	}
	if !snap.Bids[0].Price.Equal(decimal.RequireFromString("4")) {
		t.Fatalf("bid price wrong: %s", snap.Bids[0].Price)
	}
}

func TestGetKlines(t *testing.T) {
	var srv, _ = mockAster(t, map[string]string{
		"GET /fapi/v3/klines": `[[
			1499040000000, "0.01634790", "0.80000000", "0.01575800",
			"0.01577100", "148976.11427815", 1499644799999, "2434.19055334",
			308, "1756.87402397", "28.46694368", "17928899.62484339"
		]]`,
	})
	var f *futures.Client = newTestFutures(t, srv)

	var candles types.Candles
	var err error
	candles, err = f.MarketData().GetHistoricalCandles(context.Background(), "BTCUSDT", types.Timeframe1m, 1)
	if err != nil {
		t.Fatalf("GetHistoricalCandles: %v", err)
	}
	if len(candles) != 1 {
		t.Fatalf("candles = %d, want 1", len(candles))
	}
	var c types.Candle = candles[0]
	if c.OpenTime != 1499040000000 || c.CloseTime != 1499644799999 || c.Trades != 308 {
		t.Fatalf("ints wrong: %+v", c)
	}
	if !c.Open.Equal(decimal.RequireFromString("0.0163479")) {
		t.Fatalf("open wrong: %s", c.Open)
	}
}

func TestExchangeErrorPassthrough(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":-2011,"msg":"Unknown order sent."}`))
	}))
	defer srv.Close()
	var f *futures.Client = newTestFutures(t, srv)

	var err error
	err = f.Trading().CancelOrder(context.Background(), types.CancelOrderRequest{Symbol: "BTCUSDT", OrderID: 1})
	if !aster.IsExchange(err) {
		t.Fatalf("expected exchange error, got %v", err)
	}
	var apiErr *aster.Error
	if !errors.As(err, &apiErr) || apiErr.AsterCode != -2011 {
		t.Fatalf("code lost: %v", err)
	}
}
