/*
FILE: futures/types/aliases.go

DESCRIPTION:
Type aliases and constant re-exports from the neutral root types package.
Aliases (not new types) preserve identity: futures/types.SideType and the
neutral types.SideType are the same type, so values flow between layers
without conversion.
*/

package types

import commontypes "github.com/tonymontanov/go-aster/types"

// SideType — order direction. Alias.
type SideType = commontypes.SideType

const (
	SideTypeBuy  = commontypes.SideTypeBuy
	SideTypeSell = commontypes.SideTypeSell
)

// OrderType — order type. Alias.
type OrderType = commontypes.OrderType

const (
	OrderTypeLimit              = commontypes.OrderTypeLimit
	OrderTypeMarket             = commontypes.OrderTypeMarket
	OrderTypeStop               = commontypes.OrderTypeStop
	OrderTypeStopMarket         = commontypes.OrderTypeStopMarket
	OrderTypeTakeProfit         = commontypes.OrderTypeTakeProfit
	OrderTypeTakeProfitMarket   = commontypes.OrderTypeTakeProfitMarket
	OrderTypeTrailingStopMarket = commontypes.OrderTypeTrailingStopMarket
)

// TimeInForceType — time in force. Alias.
type TimeInForceType = commontypes.TimeInForceType

const (
	TimeInForceTypeGTC    = commontypes.TimeInForceTypeGTC
	TimeInForceTypeIOC    = commontypes.TimeInForceTypeIOC
	TimeInForceTypeFOK    = commontypes.TimeInForceTypeFOK
	TimeInForceTypeGTX    = commontypes.TimeInForceTypeGTX
	TimeInForceTypeHidden = commontypes.TimeInForceTypeHidden
)

// OrderStatus — order status. Alias.
type OrderStatus = commontypes.OrderStatus

const (
	OrderStatusNew             = commontypes.OrderStatusNew
	OrderStatusPartiallyFilled = commontypes.OrderStatusPartiallyFilled
	OrderStatusFilled          = commontypes.OrderStatusFilled
	OrderStatusCanceled        = commontypes.OrderStatusCanceled
	OrderStatusRejected        = commontypes.OrderStatusRejected
	OrderStatusExpired         = commontypes.OrderStatusExpired
	OrderStatusUnknown         = commontypes.OrderStatusUnknown
)

// ParseOrderStatus — order status parser. Re-export.
var ParseOrderStatus = commontypes.ParseOrderStatus

// NewOrderRespType — order placement response detail level. Alias.
type NewOrderRespType = commontypes.NewOrderRespType

const (
	NewOrderRespTypeAck    = commontypes.NewOrderRespTypeAck
	NewOrderRespTypeResult = commontypes.NewOrderRespTypeResult
)

// StpMode — self-trade prevention mode. Alias.
type StpMode = commontypes.StpMode

const (
	StpModeExpireTaker = commontypes.StpModeExpireTaker
	StpModeExpireMaker = commontypes.StpModeExpireMaker
	StpModeExpireBoth  = commontypes.StpModeExpireBoth
)

// OrderBookLevel — one order book level. Alias.
type OrderBookLevel = commontypes.OrderBookLevel

// OrderBookSnapshot — order book snapshot. Alias.
type OrderBookSnapshot = commontypes.OrderBookSnapshot

// Candle / Candles — kline data. Aliases.
type Candle = commontypes.Candle
type Candles = commontypes.Candles

// Timeframe — kline interval. Alias.
type Timeframe = commontypes.Timeframe

const (
	Timeframe1m  = commontypes.Timeframe1m
	Timeframe3m  = commontypes.Timeframe3m
	Timeframe5m  = commontypes.Timeframe5m
	Timeframe15m = commontypes.Timeframe15m
	Timeframe30m = commontypes.Timeframe30m
	Timeframe1h  = commontypes.Timeframe1h
	Timeframe2h  = commontypes.Timeframe2h
	Timeframe4h  = commontypes.Timeframe4h
	Timeframe6h  = commontypes.Timeframe6h
	Timeframe8h  = commontypes.Timeframe8h
	Timeframe12h = commontypes.Timeframe12h
	Timeframe1d  = commontypes.Timeframe1d
	Timeframe3d  = commontypes.Timeframe3d
	Timeframe1w  = commontypes.Timeframe1w
	Timeframe1M  = commontypes.Timeframe1M
)

// ValidateTimeframe — timeframe validator. Re-export.
var ValidateTimeframe = commontypes.ValidateTimeframe

// AggTrade — aggregated trade. Alias.
type AggTrade = commontypes.AggTrade

// KlineUpdate — streaming kline event. Alias.
type KlineUpdate = commontypes.KlineUpdate

// QuotedSpreadUpdate — best bid/ask update. Alias.
type QuotedSpreadUpdate = commontypes.QuotedSpreadUpdate

// RateLimitType / RateLimitInfo — exchangeInfo rate-limit descriptors. Aliases.
type RateLimitType = commontypes.RateLimitType

const (
	RateLimitTypeRequestWeight = commontypes.RateLimitTypeRequestWeight
	RateLimitTypeOrders        = commontypes.RateLimitTypeOrders
)

type RateLimitInfo = commontypes.RateLimitInfo
