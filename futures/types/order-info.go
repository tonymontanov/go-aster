/*
FILE: futures/types/order-info.go

DESCRIPTION:
Unified order representation returned by all trading and query methods
(CreateOrder / ModifyOrder / GetOrder / GetOpenOrders / ...). Field set is the
union of the Aster V3 order response payloads; fields absent from a specific
response stay at zero values.
*/

package types

import "github.com/shopspring/decimal"

// OrderInfo — order state as reported by the exchange.
type OrderInfo struct {
	// Symbol — trading pair.
	Symbol string
	// OrderID — exchange order id.
	OrderID int64
	// ClientOrderID — user-defined order id.
	ClientOrderID string
	// Side — BUY/SELL.
	Side SideType
	// PositionSide — BOTH/LONG/SHORT.
	PositionSide PositionSide
	// Type — current order type.
	Type OrderType
	// OrigType — original order type before trigger (conditional orders).
	OrigType OrderType
	// TimeInForce — time in force.
	TimeInForce TimeInForceType
	// Status — order status.
	Status OrderStatus
	// Price — order price.
	Price decimal.Decimal
	// AvgPrice — average fill price.
	AvgPrice decimal.Decimal
	// OrigQuantity — original order quantity.
	OrigQuantity decimal.Decimal
	// ExecutedQuantity — filled quantity.
	ExecutedQuantity decimal.Decimal
	// CumQuote — filled quote asset amount.
	CumQuote decimal.Decimal
	// StopPrice — trigger price (ignore for TRAILING_STOP_MARKET).
	StopPrice decimal.Decimal
	// ReduceOnly — reduce-only flag.
	ReduceOnly bool
	// ClosePosition — Close-All flag.
	ClosePosition bool
	// WorkingType — trigger price source.
	WorkingType WorkingType
	// PriceProtect — trigger protection flag.
	PriceProtect bool
	// ActivatePrice — activation price (TRAILING_STOP_MARKET only).
	ActivatePrice decimal.Decimal
	// PriceRate — callback rate (TRAILING_STOP_MARKET only).
	PriceRate decimal.Decimal
	// CreatedTime — order creation time, ms (time field of query responses;
	// for placement responses filled with UpdateTime as the closest anchor).
	CreatedTime int64
	// UpdateTime — last update time, ms.
	UpdateTime int64
	// RateLimits — X-MBX-* rate-limit headers captured from the HTTP response
	// that produced this OrderInfo. Empty for entries of list responses.
	RateLimits map[string]string
}
