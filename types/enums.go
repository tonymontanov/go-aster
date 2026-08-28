/*
FILE: types/enums.go

DESCRIPTION:
Aster protocol enums shared by the futures and (future) spot profiles.
All values are string-typed enums that exactly match the Aster V3 wire
protocol (Binance-style upper-case strings). This allows:
  - direct serialization/deserialization without mapping;
  - using values directly in URLs/JSON;
  - comparison via `==` without allocations.

SHARED HERE:
  - SideType         — BUY/SELL.
  - OrderType        — LIMIT/MARKET/STOP/STOP_MARKET/TAKE_PROFIT/
                       TAKE_PROFIT_MARKET/TRAILING_STOP_MARKET.
  - TimeInForceType  — GTC/IOC/FOK/GTX/HIDDEN.
  - OrderStatus      — NEW/PARTIALLY_FILLED/FILLED/CANCELED/REJECTED/EXPIRED
                       + ParseOrderStatus.
  - NewOrderRespType — ACK/RESULT.
  - StpMode          — self-trade prevention modes.

REMAINS IN futures/types (profile-specific):
  - PositionSide, WorkingType, MarginType, ContractType, ContractStatus.
*/

package types

// SideType — order direction. Shared across all Aster profiles.
type SideType string

const (
	// SideTypeBuy — buy.
	SideTypeBuy SideType = "BUY"
	// SideTypeSell — sell.
	SideTypeSell SideType = "SELL"
)

// OrderType — Aster order type notation (Binance-style). Conditional types
// (STOP/TAKE_PROFIT families, trailing) are expressed as separate OrderType
// values rather than flags — this matches the wire protocol.
type OrderType string

const (
	// OrderTypeLimit — limit order (requires timeInForce, quantity, price).
	OrderTypeLimit OrderType = "LIMIT"
	// OrderTypeMarket — market order (requires quantity).
	OrderTypeMarket OrderType = "MARKET"
	// OrderTypeStop — stop-limit (requires quantity, price, stopPrice).
	OrderTypeStop OrderType = "STOP"
	// OrderTypeStopMarket — stop-market (requires stopPrice).
	OrderTypeStopMarket OrderType = "STOP_MARKET"
	// OrderTypeTakeProfit — take-profit limit (requires quantity, price, stopPrice).
	OrderTypeTakeProfit OrderType = "TAKE_PROFIT"
	// OrderTypeTakeProfitMarket — take-profit market (requires stopPrice).
	OrderTypeTakeProfitMarket OrderType = "TAKE_PROFIT_MARKET"
	// OrderTypeTrailingStopMarket — trailing stop market (requires callbackRate).
	OrderTypeTrailingStopMarket OrderType = "TRAILING_STOP_MARKET"
)

// TimeInForceType — TIF in Aster/Binance notation. Sent to the exchange as is.
type TimeInForceType string

const (
	// TimeInForceTypeGTC — Good Till Cancel.
	TimeInForceTypeGTC TimeInForceType = "GTC"
	// TimeInForceTypeIOC — Immediate or Cancel.
	TimeInForceTypeIOC TimeInForceType = "IOC"
	// TimeInForceTypeFOK — Fill or Kill.
	TimeInForceTypeFOK TimeInForceType = "FOK"
	// TimeInForceTypeGTX — Good Till Crossing (Post Only).
	TimeInForceTypeGTX TimeInForceType = "GTX"
	// TimeInForceTypeHidden — HIDDEN: the order is not visible in the order
	// book. Aster-specific extension of the Binance TIF set.
	TimeInForceTypeHidden TimeInForceType = "HIDDEN"
)

// OrderStatus — order status enum, matches the wire values.
type OrderStatus string

const (
	// OrderStatusNew — active, waiting for execution.
	OrderStatusNew OrderStatus = "NEW"
	// OrderStatusPartiallyFilled — partially filled, remainder active.
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	// OrderStatusFilled — fully filled.
	OrderStatusFilled OrderStatus = "FILLED"
	// OrderStatusCanceled — canceled by the user.
	OrderStatusCanceled OrderStatus = "CANCELED"
	// OrderStatusRejected — rejected by the exchange.
	OrderStatusRejected OrderStatus = "REJECTED"
	// OrderStatusExpired — expired (e.g. GTX crossed the book, IOC remainder).
	OrderStatusExpired OrderStatus = "EXPIRED"
	// OrderStatusUnknown — status could not be parsed (defensive fallback).
	OrderStatusUnknown OrderStatus = "UNKNOWN"
)

// ParseOrderStatus parses an Aster order status string into a typed enum.
// Anything unknown → OrderStatusUnknown (log at the call site for diagnostics).
func ParseOrderStatus(s string) OrderStatus {
	switch s {
	case "NEW":
		return OrderStatusNew
	case "PARTIALLY_FILLED":
		return OrderStatusPartiallyFilled
	case "FILLED":
		return OrderStatusFilled
	case "CANCELED":
		return OrderStatusCanceled
	case "REJECTED":
		return OrderStatusRejected
	case "EXPIRED":
		return OrderStatusExpired
	default:
		return OrderStatusUnknown
	}
}

// IsTerminal returns true when the status is final (the order will never
// trade again): FILLED, CANCELED, REJECTED, EXPIRED.
func (s OrderStatus) IsTerminal() bool {
	switch s {
	case OrderStatusFilled, OrderStatusCanceled, OrderStatusRejected, OrderStatusExpired:
		return true
	default:
		return false
	}
}

// NewOrderRespType — response detail level for order placement.
type NewOrderRespType string

const (
	// NewOrderRespTypeAck — minimal acknowledgement (default).
	NewOrderRespTypeAck NewOrderRespType = "ACK"
	// NewOrderRespTypeResult — final order state in the placement response
	// (for MARKET / special-TIF LIMIT orders).
	NewOrderRespTypeResult NewOrderRespType = "RESULT"
)

// StpMode — self-trade prevention mode.
type StpMode string

const (
	// StpModeExpireTaker — cancel the taker side of a would-be self-trade.
	StpModeExpireTaker StpMode = "EXPIRE_TAKER"
	// StpModeExpireMaker — cancel the maker side.
	StpModeExpireMaker StpMode = "EXPIRE_MAKER"
	// StpModeExpireBoth — cancel both sides.
	StpModeExpireBoth StpMode = "EXPIRE_BOTH"
)
