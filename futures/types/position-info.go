/*
FILE: futures/types/position-info.go

DESCRIPTION:
Position state from GET /fapi/v3/positionRisk. One entry per (symbol,
positionSide): a single BOTH entry in One-way mode, LONG + SHORT entries in
Hedge mode.
*/

package types

import "github.com/shopspring/decimal"

// PositionInfo — position state for one (symbol, positionSide).
type PositionInfo struct {
	// Symbol — trading pair.
	Symbol string
	// PositionSide — BOTH/LONG/SHORT.
	PositionSide PositionSide
	// PositionAmt — signed position size in base asset (negative = short).
	PositionAmt decimal.Decimal
	// EntryPrice — average entry price.
	EntryPrice decimal.Decimal
	// MarkPrice — current mark price.
	MarkPrice decimal.Decimal
	// UnrealizedProfit — unrealized PnL.
	UnrealizedProfit decimal.Decimal
	// LiquidationPrice — estimated liquidation price (0 when no position).
	LiquidationPrice decimal.Decimal
	// Leverage — current initial leverage.
	Leverage int64
	// MarginType — ISOLATED/CROSSED.
	MarginType MarginType
	// IsAutoAddMargin — auto add margin enabled (isolated positions).
	IsAutoAddMargin bool
	// IsolatedMargin — isolated margin amount.
	IsolatedMargin decimal.Decimal
	// MaxNotionalValue — max notional with current leverage.
	MaxNotionalValue decimal.Decimal
	// UpdateTime — last update time, ms.
	UpdateTime int64
}
