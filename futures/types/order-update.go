/*
FILE: futures/types/order-update.go

DESCRIPTION:
Order update from the user data stream (ORDER_TRADE_UPDATE event). One event
per order state transition; TRADE executions additionally carry fill fields
(LastFilledQuantity/Price, Commission, RealizedProfit, TradeID).
*/

package types

import "github.com/shopspring/decimal"

// ExecutionType — order state transition type (x field).
type ExecutionType string

const (
	// ExecutionTypeNew — order accepted.
	ExecutionTypeNew ExecutionType = "NEW"
	// ExecutionTypeCanceled — order canceled.
	ExecutionTypeCanceled ExecutionType = "CANCELED"
	// ExecutionTypeCalculated — liquidation execution.
	ExecutionTypeCalculated ExecutionType = "CALCULATED"
	// ExecutionTypeExpired — order expired.
	ExecutionTypeExpired ExecutionType = "EXPIRED"
	// ExecutionTypeTrade — fill.
	ExecutionTypeTrade ExecutionType = "TRADE"
)

// OrderUpdate — one ORDER_TRADE_UPDATE event.
type OrderUpdate struct {
	// EventTs / TxTs — event and transaction times, ms.
	EventTs int64
	TxTs    int64
	// Symbol — trading pair.
	Symbol string
	// ClientOrderID — client order id. Special values: prefix "autoclose-" —
	// liquidation order, "adl_autoclose" — ADL auto-close order.
	ClientOrderID string
	// OrderID — exchange order id.
	OrderID int64
	// Side — BUY/SELL.
	Side SideType
	// PositionSide — BOTH/LONG/SHORT.
	PositionSide PositionSide
	// Type — current order type; OrigType — type before trigger.
	Type     OrderType
	OrigType OrderType
	// TimeInForce — time in force.
	TimeInForce TimeInForceType
	// ExecutionType — transition type (NEW/CANCELED/CALCULATED/EXPIRED/TRADE).
	ExecutionType ExecutionType
	// Status — order status after the transition.
	Status OrderStatus
	// OrigQuantity / Price — original order quantity and price.
	OrigQuantity decimal.Decimal
	Price        decimal.Decimal
	// AvgPrice — average fill price.
	AvgPrice decimal.Decimal
	// StopPrice — trigger price (ignore for TRAILING_STOP_MARKET).
	StopPrice decimal.Decimal
	// LastFilledQuantity / LastFilledPrice — the fill of THIS event (TRADE).
	LastFilledQuantity decimal.Decimal
	LastFilledPrice    decimal.Decimal
	// FilledAccumulatedQuantity — total filled so far.
	FilledAccumulatedQuantity decimal.Decimal
	// Commission / CommissionAsset — fee of this fill (absent when zero).
	Commission      decimal.Decimal
	CommissionAsset string
	// TradeTime — trade time of this event, ms.
	TradeTime int64
	// TradeID — trade id (TRADE executions).
	TradeID int64
	// BidsNotional / AsksNotional — open notional per side.
	BidsNotional decimal.Decimal
	AsksNotional decimal.Decimal
	// IsMaker — true when this fill was maker.
	IsMaker bool
	// IsReduceOnly — reduce-only flag.
	IsReduceOnly bool
	// WorkingType — trigger price source.
	WorkingType WorkingType
	// IsClosePosition — Close-All conditional order flag.
	IsClosePosition bool
	// ActivationPrice / CallbackRate — TRAILING_STOP_MARKET only.
	ActivationPrice decimal.Decimal
	CallbackRate    decimal.Decimal
	// RealizedProfit — realized PnL of this fill.
	RealizedProfit decimal.Decimal
}
