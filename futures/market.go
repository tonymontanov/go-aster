/*
FILE: futures/market.go

DESCRIPTION:
Market data domain client of the Futures profile: connectivity, exchange
info / symbol rules, order book snapshot, klines, mark price, funding
history, tickers.

ASTER SPECIFICS:
  - GET /fapi/v3/exchangeInfo — no symbol filter parameter; the SDK caches the
    full response for a short TTL and filters client-side in GetSymbolInfo;
  - GET /fapi/v3/depth — snapshot limits: 5, 10, 20, 50, 100, 500, 1000;
  - klines arrive as positional JSON arrays and are decoded into types.Candle.

CONCURRENCY:
Safe for concurrent use; the exchangeInfo cache is guarded by a mutex.
*/

package futures

import (
	"context"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	aster "github.com/tonymontanov/go-aster"
	"github.com/tonymontanov/go-aster/futures/types"
	"github.com/tonymontanov/go-aster/internal/codec"
	"github.com/tonymontanov/go-aster/internal/rest"
)

// Futures market data endpoint paths.
const (
	endpointPing         = "/fapi/v3/ping"
	endpointServerTime   = "/fapi/v3/time"
	endpointExchangeInfo = "/fapi/v3/exchangeInfo"
	endpointDepth        = "/fapi/v3/depth"
	endpointKlines       = "/fapi/v3/klines"
	endpointPremiumIndex = "/fapi/v3/premiumIndex"
	endpointFundingRate  = "/fapi/v3/fundingRate"
	endpointBookTicker   = "/fapi/v3/ticker/bookTicker"
	endpointPriceTicker  = "/fapi/v3/ticker/price"
)

// exchangeInfoCacheTTL — how long a fetched exchangeInfo stays fresh for
// GetSymbolInfo. Trading rules change rarely; 30s keeps SymbolInfo lookups
// cheap without risking stale filters during symbol maintenance windows.
const exchangeInfoCacheTTL = 30 * time.Second

// depthLimits — snapshot limits accepted by GET /fapi/v3/depth.
var depthLimits = []int{5, 10, 20, 50, 100, 500, 1000}

// MarketDataClient — market data domain client.
type MarketDataClient struct {
	c *Client

	// exchangeInfo cache for GetSymbolInfo.
	mu        sync.Mutex
	cached    *types.ExchangeInfo
	fetchedAt time.Time
}

// newMarketDataClient — internal constructor.
func newMarketDataClient(c *Client) *MarketDataClient {
	return &MarketDataClient{c: c}
}

// Ping tests REST connectivity (GET /fapi/v3/ping).
func (m *MarketDataClient) Ping(ctx context.Context) error {
	var err error
	_, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointPing,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryMarketData)},
	})
	return err
}

// GetServerTime returns the exchange server time in ms (GET /fapi/v3/time).
func (m *MarketDataClient) GetServerTime(ctx context.Context) (int64, error) {
	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointServerTime,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryMarketData)},
	})
	if err != nil {
		return 0, err
	}

	var raw struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err = resp.Unmarshal(&raw); err != nil {
		return 0, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetServerTime: parse", err)
	}
	return raw.ServerTime, nil
}

// GetExchangeInfo returns exchange-wide trading rules
// (GET /fapi/v3/exchangeInfo). Always hits the network; GetSymbolInfo uses a
// short-TTL cache on top.
func (m *MarketDataClient) GetExchangeInfo(ctx context.Context) (types.ExchangeInfo, error) {
	var out types.ExchangeInfo

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointExchangeInfo,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryMarketData)},
	})
	if err != nil {
		return out, err
	}

	var raw rawExchangeInfo
	if err = resp.Unmarshal(&raw); err != nil {
		return out, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetExchangeInfo: parse", err)
	}
	out = convertExchangeInfo(raw)

	m.mu.Lock()
	m.cached = &out
	m.fetchedAt = time.Now()
	m.mu.Unlock()
	return out, nil
}

/*
GetSymbolInfo returns trading rules of one symbol. exchangeInfo has no symbol
filter parameter, so the full response is cached for exchangeInfoCacheTTL and
filtered client-side. A symbol missing from a fresh response yields
ErrorKindInvalidRequest.
*/
func (m *MarketDataClient) GetSymbolInfo(ctx context.Context, symbol string) (types.SymbolInfo, error) {
	var out types.SymbolInfo
	if symbol == "" {
		return out, aster.NewError(aster.ErrorKindInvalidRequest, 0, "market.GetSymbolInfo: symbol is empty", nil)
	}

	m.mu.Lock()
	var cached *types.ExchangeInfo = m.cached
	var fresh bool = cached != nil && time.Since(m.fetchedAt) < exchangeInfoCacheTTL
	m.mu.Unlock()

	if !fresh {
		var info types.ExchangeInfo
		var err error
		info, err = m.GetExchangeInfo(ctx)
		if err != nil {
			return out, err
		}
		cached = &info
	}

	var i int
	for i = 0; i < len(cached.Symbols); i++ {
		if cached.Symbols[i].Symbol == symbol {
			return cached.Symbols[i], nil
		}
	}
	return out, aster.NewError(aster.ErrorKindInvalidRequest, 0, "market.GetSymbolInfo: unknown symbol "+symbol, nil)
}

/*
GetOrderBook returns an order book snapshot (GET /fapi/v3/depth).
limit must be one of 5, 10, 20, 50, 100, 500, 1000; other values are clamped
UP to the nearest allowed limit (the caller never receives less depth than
requested).
*/
func (m *MarketDataClient) GetOrderBook(ctx context.Context, symbol string, limit int) (types.OrderBookSnapshot, error) {
	var out types.OrderBookSnapshot
	if symbol == "" {
		return out, aster.NewError(aster.ErrorKindInvalidRequest, 0, "market.GetOrderBook: symbol is empty", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)
	params.Set("limit", strconv.Itoa(clampDepthLimit(limit)))

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointDepth,
		Params: params,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return out, err
	}

	var raw rawDepth
	if err = resp.Unmarshal(&raw); err != nil {
		return out, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetOrderBook: parse", err)
	}

	out.Symbol = symbol
	out.LastUpdateID = raw.LastUpdateID
	out.EventTs = raw.EventTime
	out.TxTs = raw.TxTime
	out.Bids, err = parseBookLevels(raw.Bids)
	if err != nil {
		return out, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetOrderBook: parse bids", err)
	}
	out.Asks, err = parseBookLevels(raw.Asks)
	if err != nil {
		return out, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetOrderBook: parse asks", err)
	}
	return out, nil
}

/*
GetKlines returns candles (GET /fapi/v3/klines). Optional: startTimeMs /
endTimeMs window, limit (default 500, max 1500). Zero values are omitted.
Candles are returned oldest-first (exchange order).
*/
func (m *MarketDataClient) GetKlines(ctx context.Context, symbol string, timeframe types.Timeframe, startTimeMs, endTimeMs int64, limit int) (types.Candles, error) {
	if symbol == "" {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "market.GetKlines: symbol is empty", nil)
	}
	if !types.ValidateTimeframe(timeframe) {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "market.GetKlines: invalid timeframe "+string(timeframe), nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", string(timeframe))
	if startTimeMs != 0 {
		params.Set("startTime", strconv.FormatInt(startTimeMs, 10))
	}
	if endTimeMs != 0 {
		params.Set("endTime", strconv.FormatInt(endTimeMs, 10))
	}
	if limit != 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointKlines,
		Params: params,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return nil, err
	}

	var rows [][]any
	if err = resp.Unmarshal(&rows); err != nil {
		return nil, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetKlines: parse", err)
	}

	var candles types.Candles = make(types.Candles, 0, len(rows))
	var i int
	for i = 0; i < len(rows); i++ {
		var candle types.Candle
		candle, err = parseKlineRow(rows[i])
		if err != nil {
			return nil, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetKlines: parse row", err)
		}
		candles = append(candles, candle)
	}
	return candles, nil
}

// GetHistoricalCandles returns the latest `length` candles of the timeframe
// (thin wrapper over GetKlines, spec §Use cases).
func (m *MarketDataClient) GetHistoricalCandles(ctx context.Context, symbol string, timeframe types.Timeframe, length int) (types.Candles, error) {
	if length <= 0 {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "market.GetHistoricalCandles: length must be positive", nil)
	}
	return m.GetKlines(ctx, symbol, timeframe, 0, 0, length)
}

// GetMarkPrice returns mark price / funding state of the symbol
// (GET /fapi/v3/premiumIndex).
func (m *MarketDataClient) GetMarkPrice(ctx context.Context, symbol string) (types.MarkPriceInfo, error) {
	var out types.MarkPriceInfo
	if symbol == "" {
		return out, aster.NewError(aster.ErrorKindInvalidRequest, 0, "market.GetMarkPrice: symbol is empty", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointPremiumIndex,
		Params: params,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return out, err
	}

	var raw rawMarkPrice
	if err = resp.Unmarshal(&raw); err != nil {
		return out, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetMarkPrice: parse", err)
	}
	return convertMarkPrice(raw), nil
}

/*
GetFundingRateHistory returns historical funding rates
(GET /fapi/v3/fundingRate). Optional: startTimeMs/endTimeMs window, limit
(default 100, max 1000). Zero values are omitted.
*/
func (m *MarketDataClient) GetFundingRateHistory(ctx context.Context, symbol string, startTimeMs, endTimeMs int64, limit int) ([]types.FundingRateEntry, error) {
	var params url.Values = url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	if startTimeMs != 0 {
		params.Set("startTime", strconv.FormatInt(startTimeMs, 10))
	}
	if endTimeMs != 0 {
		params.Set("endTime", strconv.FormatInt(endTimeMs, 10))
	}
	if limit != 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointFundingRate,
		Params: params,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryMarketData)},
	})
	if err != nil {
		return nil, err
	}

	var raws []struct {
		Symbol      string `json:"symbol"`
		FundingRate string `json:"fundingRate"`
		FundingTime int64  `json:"fundingTime"`
	}
	if err = resp.Unmarshal(&raws); err != nil {
		return nil, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetFundingRateHistory: parse", err)
	}

	var out []types.FundingRateEntry = make([]types.FundingRateEntry, 0, len(raws))
	var i int
	for i = 0; i < len(raws); i++ {
		var entry types.FundingRateEntry = types.FundingRateEntry{
			Symbol:      raws[i].Symbol,
			FundingTime: raws[i].FundingTime,
		}
		entry.FundingRate, _ = codec.ParseDecimal(raws[i].FundingRate)
		out = append(out, entry)
	}
	return out, nil
}

// GetBookTicker returns the current best bid/ask of the symbol
// (GET /fapi/v3/ticker/bookTicker).
func (m *MarketDataClient) GetBookTicker(ctx context.Context, symbol string) (types.QuotedSpreadUpdate, error) {
	var out types.QuotedSpreadUpdate
	if symbol == "" {
		return out, aster.NewError(aster.ErrorKindInvalidRequest, 0, "market.GetBookTicker: symbol is empty", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointBookTicker,
		Params: params,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return out, err
	}

	var raw struct {
		Symbol   string `json:"symbol"`
		BidPrice string `json:"bidPrice"`
		BidQty   string `json:"bidQty"`
		AskPrice string `json:"askPrice"`
		AskQty   string `json:"askQty"`
		Time     int64  `json:"time"`
	}
	if err = resp.Unmarshal(&raw); err != nil {
		return out, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetBookTicker: parse", err)
	}
	out.Symbol = raw.Symbol
	out.EventTs = raw.Time
	out.BestBidPrice, _ = codec.ParseDecimal(raw.BidPrice)
	out.BestBidQty, _ = codec.ParseDecimal(raw.BidQty)
	out.BestAskPrice, _ = codec.ParseDecimal(raw.AskPrice)
	out.BestAskQty, _ = codec.ParseDecimal(raw.AskQty)
	return out, nil
}

// GetPrice returns the latest price of the symbol (GET /fapi/v3/ticker/price).
func (m *MarketDataClient) GetPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	if symbol == "" {
		return decimal.Zero, aster.NewError(aster.ErrorKindInvalidRequest, 0, "market.GetPrice: symbol is empty", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointPriceTicker,
		Params: params,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return decimal.Zero, err
	}

	var raw struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}
	if err = resp.Unmarshal(&raw); err != nil {
		return decimal.Zero, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetPrice: parse", err)
	}
	var price decimal.Decimal
	price, err = codec.ParseDecimal(raw.Price)
	if err != nil {
		return decimal.Zero, aster.NewError(aster.ErrorKindUnknown, 0, "market.GetPrice: parse price", err)
	}
	return price, nil
}

// clampDepthLimit rounds a requested depth UP to the nearest allowed limit
// (or the maximum when the request exceeds it).
func clampDepthLimit(limit int) int {
	if limit <= 0 {
		return depthLimits[len(depthLimits)-1]
	}
	var i int
	for i = 0; i < len(depthLimits); i++ {
		if limit <= depthLimits[i] {
			return depthLimits[i]
		}
	}
	return depthLimits[len(depthLimits)-1]
}

// rawDepth — wire depth snapshot.
type rawDepth struct {
	LastUpdateID int64       `json:"lastUpdateId"`
	EventTime    int64       `json:"E"`
	TxTime       int64       `json:"T"`
	Bids         [][2]string `json:"bids"`
	Asks         [][2]string `json:"asks"`
}

// parseBookLevels converts wire ["price","qty"] pairs into typed levels.
func parseBookLevels(raw [][2]string) ([]types.OrderBookLevel, error) {
	var out []types.OrderBookLevel = make([]types.OrderBookLevel, 0, len(raw))
	var i int
	for i = 0; i < len(raw); i++ {
		var lvl types.OrderBookLevel
		var err error
		lvl.Price, err = codec.ParseDecimal(raw[i][0])
		if err != nil {
			return nil, err
		}
		lvl.Size, err = codec.ParseDecimal(raw[i][1])
		if err != nil {
			return nil, err
		}
		out = append(out, lvl)
	}
	return out, nil
}

// parseKlineRow converts one positional kline array into a Candle.
// Wire layout: [openTime, "o", "h", "l", "c", "v", closeTime, "quoteVolume",
// trades, "takerBuyBase", "takerBuyQuote", "ignore"].
func parseKlineRow(row []any) (types.Candle, error) {
	var candle types.Candle
	if len(row) < 11 {
		return candle, aster.NewError(aster.ErrorKindUnknown, 0, "kline row too short", nil)
	}
	candle.OpenTime = asInt64(row[0])
	candle.CloseTime = asInt64(row[6])
	candle.Trades = asInt64(row[8])
	var err error
	if candle.Open, err = codec.ParseDecimal(asString(row[1])); err != nil {
		return candle, err
	}
	if candle.High, err = codec.ParseDecimal(asString(row[2])); err != nil {
		return candle, err
	}
	if candle.Low, err = codec.ParseDecimal(asString(row[3])); err != nil {
		return candle, err
	}
	if candle.Close, err = codec.ParseDecimal(asString(row[4])); err != nil {
		return candle, err
	}
	if candle.Volume, err = codec.ParseDecimal(asString(row[5])); err != nil {
		return candle, err
	}
	if candle.QuoteVolume, err = codec.ParseDecimal(asString(row[7])); err != nil {
		return candle, err
	}
	if candle.TakerBuyBaseVolume, err = codec.ParseDecimal(asString(row[9])); err != nil {
		return candle, err
	}
	if candle.TakerBuyQuoteVolume, err = codec.ParseDecimal(asString(row[10])); err != nil {
		return candle, err
	}
	return candle, nil
}

// asInt64 coerces a decoded JSON number (float64 in any-typed rows) to int64.
// Millisecond timestamps and trade counts fit float64 exactly (< 2^53).
func asInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

// asString coerces a decoded JSON value to string ("" for non-strings).
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// rawMarkPrice — wire premiumIndex payload.
type rawMarkPrice struct {
	Symbol               string `json:"symbol"`
	MarkPrice            string `json:"markPrice"`
	IndexPrice           string `json:"indexPrice"`
	EstimatedSettlePrice string `json:"estimatedSettlePrice"`
	LastFundingRate      string `json:"lastFundingRate"`
	NextFundingTime      int64  `json:"nextFundingTime"`
	InterestRate         string `json:"interestRate"`
	Time                 int64  `json:"time"`
}

// convertMarkPrice maps a rawMarkPrice into the domain MarkPriceInfo.
func convertMarkPrice(raw rawMarkPrice) types.MarkPriceInfo {
	var out types.MarkPriceInfo = types.MarkPriceInfo{
		Symbol:          raw.Symbol,
		NextFundingTime: raw.NextFundingTime,
		Ts:              raw.Time,
	}
	out.MarkPrice, _ = codec.ParseDecimal(raw.MarkPrice)
	out.IndexPrice, _ = codec.ParseDecimal(raw.IndexPrice)
	out.EstimatedSettlePrice, _ = codec.ParseDecimal(raw.EstimatedSettlePrice)
	out.LastFundingRate, _ = codec.ParseDecimal(raw.LastFundingRate)
	out.InterestRate, _ = codec.ParseDecimal(raw.InterestRate)
	return out
}

// rawExchangeInfo — wire exchangeInfo payload (fields the SDK exposes).
type rawExchangeInfo struct {
	Timezone   string `json:"timezone"`
	ServerTime int64  `json:"serverTime"`
	RateLimits []struct {
		RateLimitType string `json:"rateLimitType"`
		Interval      string `json:"interval"`
		IntervalNum   int    `json:"intervalNum"`
		Limit         int64  `json:"limit"`
	} `json:"rateLimits"`
	Assets []struct {
		Asset           string `json:"asset"`
		MarginAvailable bool   `json:"marginAvailable"`
	} `json:"assets"`
	Symbols []rawSymbolInfo `json:"symbols"`
}

// rawSymbolInfo — wire symbols[] entry. Filters are decoded generically and
// flattened in convertSymbolInfo. Note the exchange quirk: the order types
// array is capitalized "OrderType" on the wire.
type rawSymbolInfo struct {
	Symbol            string      `json:"symbol"`
	Pair              string      `json:"pair"`
	ContractType      string      `json:"contractType"`
	Status            string      `json:"status"`
	BaseAsset         string      `json:"baseAsset"`
	QuoteAsset        string      `json:"quoteAsset"`
	MarginAsset       string      `json:"marginAsset"`
	PricePrecision    int         `json:"pricePrecision"`
	QuantityPrecision int         `json:"quantityPrecision"`
	TriggerProtect    string      `json:"triggerProtect"`
	LiquidationFee    string      `json:"liquidationFee"`
	MarketTakeBound   string      `json:"marketTakeBound"`
	OrderTypes        []string    `json:"OrderType"`
	TimeInForce       []string    `json:"timeInForce"`
	Filters           []rawFilter `json:"filters"`
}

// rawFilter — one filters[] entry; the union of all filter type fields.
type rawFilter struct {
	FilterType     string `json:"filterType"`
	MinPrice       string `json:"minPrice"`
	MaxPrice       string `json:"maxPrice"`
	TickSize       string `json:"tickSize"`
	MinQty         string `json:"minQty"`
	MaxQty         string `json:"maxQty"`
	StepSize       string `json:"stepSize"`
	Notional       string `json:"notional"`
	MultiplierUp   string `json:"multiplierUp"`
	MultiplierDown string `json:"multiplierDown"`
	Limit          int64  `json:"limit"`
}

// convertExchangeInfo maps a rawExchangeInfo into the domain ExchangeInfo.
func convertExchangeInfo(raw rawExchangeInfo) types.ExchangeInfo {
	var out types.ExchangeInfo = types.ExchangeInfo{
		Timezone:   raw.Timezone,
		ServerTime: raw.ServerTime,
	}

	var i int
	out.RateLimits = make([]types.RateLimitInfo, 0, len(raw.RateLimits))
	for i = 0; i < len(raw.RateLimits); i++ {
		out.RateLimits = append(out.RateLimits, types.RateLimitInfo{
			RateLimitType: types.RateLimitType(raw.RateLimits[i].RateLimitType),
			Interval:      raw.RateLimits[i].Interval,
			IntervalNum:   raw.RateLimits[i].IntervalNum,
			Limit:         raw.RateLimits[i].Limit,
		})
	}

	out.Assets = make([]types.AssetInfo, 0, len(raw.Assets))
	for i = 0; i < len(raw.Assets); i++ {
		out.Assets = append(out.Assets, types.AssetInfo{
			Asset:           raw.Assets[i].Asset,
			MarginAvailable: raw.Assets[i].MarginAvailable,
		})
	}

	out.Symbols = make([]types.SymbolInfo, 0, len(raw.Symbols))
	for i = 0; i < len(raw.Symbols); i++ {
		out.Symbols = append(out.Symbols, convertSymbolInfo(raw.Symbols[i]))
	}
	return out
}

// convertSymbolInfo maps a rawSymbolInfo into the domain SymbolInfo,
// flattening the filters array into named fields.
func convertSymbolInfo(raw rawSymbolInfo) types.SymbolInfo {
	var out types.SymbolInfo = types.SymbolInfo{
		Symbol:            raw.Symbol,
		Pair:              raw.Pair,
		ContractType:      types.ContractType(raw.ContractType),
		Status:            types.ContractStatus(raw.Status),
		BaseAsset:         raw.BaseAsset,
		QuoteAsset:        raw.QuoteAsset,
		MarginAsset:       raw.MarginAsset,
		PricePrecision:    raw.PricePrecision,
		QuantityPrecision: raw.QuantityPrecision,
	}
	out.TriggerProtect, _ = codec.ParseDecimal(raw.TriggerProtect)
	out.LiquidationFee, _ = codec.ParseDecimal(raw.LiquidationFee)
	out.MarketTakeBound, _ = codec.ParseDecimal(raw.MarketTakeBound)

	var i int
	out.OrderTypes = make([]types.OrderType, 0, len(raw.OrderTypes))
	for i = 0; i < len(raw.OrderTypes); i++ {
		out.OrderTypes = append(out.OrderTypes, types.OrderType(raw.OrderTypes[i]))
	}
	out.TimeInForce = make([]types.TimeInForceType, 0, len(raw.TimeInForce))
	for i = 0; i < len(raw.TimeInForce); i++ {
		out.TimeInForce = append(out.TimeInForce, types.TimeInForceType(raw.TimeInForce[i]))
	}

	for i = 0; i < len(raw.Filters); i++ {
		var f rawFilter = raw.Filters[i]
		switch f.FilterType {
		case "PRICE_FILTER":
			out.MinPrice, _ = codec.ParseDecimal(f.MinPrice)
			out.MaxPrice, _ = codec.ParseDecimal(f.MaxPrice)
			out.TickSize, _ = codec.ParseDecimal(f.TickSize)
		case "LOT_SIZE":
			out.MinQuantity, _ = codec.ParseDecimal(f.MinQty)
			out.MaxQuantity, _ = codec.ParseDecimal(f.MaxQty)
			out.StepSize, _ = codec.ParseDecimal(f.StepSize)
		case "MARKET_LOT_SIZE":
			out.MarketMinQuantity, _ = codec.ParseDecimal(f.MinQty)
			out.MarketMaxQuantity, _ = codec.ParseDecimal(f.MaxQty)
			out.MarketStepSize, _ = codec.ParseDecimal(f.StepSize)
		case "MIN_NOTIONAL":
			out.MinNotional, _ = codec.ParseDecimal(f.Notional)
		case "PERCENT_PRICE":
			out.MultiplierUp, _ = codec.ParseDecimal(f.MultiplierUp)
			out.MultiplierDown, _ = codec.ParseDecimal(f.MultiplierDown)
		case "MAX_NUM_ORDERS":
			out.MaxNumOrders = f.Limit
		}
	}
	return out
}
