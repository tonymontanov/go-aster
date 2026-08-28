/*
FILE: futures/types/trade.go

DESCRIPTION:
Account trade (fill) from GET /fapi/v3/userTrades.
*/

package types

import "github.com/shopspring/decimal"

// TradeInfo — one account trade (fill).
type TradeInfo struct {
	// Symbol — trading pair.
	Symbol string
	// ID — trade id.
	ID int64
	// OrderID — order that produced the fill.
	OrderID int64
	// Side — BUY/SELL.
	Side SideType
	// PositionSide — BOTH/LONG/SHORT.
	PositionSide PositionSide
	// Price — fill price.
	Price decimal.Decimal
	// Quantity — fill quantity (base asset).
	Quantity decimal.Decimal
	// QuoteQuantity — fill amount (quote asset).
	QuoteQuantity decimal.Decimal
	// RealizedPnl — realized PnL of the fill.
	RealizedPnl decimal.Decimal
	// Commission — commission amount.
	Commission decimal.Decimal
	// CommissionAsset — commission asset.
	CommissionAsset string
	// Time — trade time, ms.
	Time int64
	// Maker — true when the fill was maker.
	Maker bool
	// Buyer — true when the account was the buyer.
	Buyer bool
}

// CommissionRate — maker/taker commission rates of a symbol
// (GET /fapi/v3/commissionRate).
type CommissionRate struct {
	// Symbol — trading pair.
	Symbol string
	// MakerCommissionRate — maker rate (e.g. 0.0002 means 0.02%).
	MakerCommissionRate decimal.Decimal
	// TakerCommissionRate — taker rate.
	TakerCommissionRate decimal.Decimal
}
