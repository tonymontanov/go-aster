/*
FILE: futures/types/create-order-request.go

DESCRIPTION:
Order creation request for POST /fapi/v3/order. Field semantics follow the
Aster V3 (Binance-style) protocol; the SDK validates the combination locally
before hitting the network (see futures/trading.go buildCreateOrderParams).

CONVENTIONS:
  - decimal zero values mean "not set" and are omitted from the wire payload;
  - ClientOrderID empty → the exchange generates one (rule ^[\.A-Z\:/a-z0-9_-]{1,36}$);
  - booleans are sent as "true"/"false" strings per the protocol.
*/

package types

import "github.com/shopspring/decimal"

// CreateOrderRequest — parameters for creating an order.
type CreateOrderRequest struct {
	// Symbol — trading pair (e.g. "BTCUSDT"). Required.
	Symbol string
	// Side — BUY/SELL. Required.
	Side SideType
	// Type — order type. Required.
	Type OrderType
	// PositionSide — BOTH (One-way, default) or LONG/SHORT (mandatory in
	// Hedge mode). Empty → not sent.
	PositionSide PositionSide
	// TimeInForce — required for LIMIT/STOP/TAKE_PROFIT types.
	TimeInForce TimeInForceType
	// Quantity — order quantity in base asset. Required except for
	// ClosePosition orders.
	Quantity decimal.Decimal
	// Price — limit price. Required for LIMIT/STOP/TAKE_PROFIT.
	Price decimal.Decimal
	// ClientOrderID — user-defined order id (newClientOrderId). Empty → the
	// exchange auto-generates one.
	ClientOrderID string
	// ReduceOnly — reduce-only flag. Cannot be used in Hedge mode or together
	// with ClosePosition.
	ReduceOnly bool
	// ClosePosition — Close-All flag, used with STOP_MARKET/TAKE_PROFIT_MARKET.
	// Incompatible with Quantity and ReduceOnly.
	ClosePosition bool
	// StopPrice — trigger price for STOP/STOP_MARKET/TAKE_PROFIT/
	// TAKE_PROFIT_MARKET.
	StopPrice decimal.Decimal
	// ActivationPrice — activation price for TRAILING_STOP_MARKET
	// (default: latest price).
	ActivationPrice decimal.Decimal
	// CallbackRate — callback rate for TRAILING_STOP_MARKET, percent
	// (0.1 .. 5 where 1 means 1%).
	CallbackRate decimal.Decimal
	// WorkingType — stopPrice trigger source: MARK_PRICE or CONTRACT_PRICE
	// (default). Empty → not sent.
	WorkingType WorkingType
	// PriceProtect — conditional order trigger protection.
	PriceProtect bool
	// NewOrderRespType — ACK (default) or RESULT.
	NewOrderRespType NewOrderRespType
	// StpMode — per-order self-trade prevention override. Empty → account
	// default applies.
	StpMode StpMode
}
