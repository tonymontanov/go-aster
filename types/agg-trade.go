/*
FILE: types/agg-trade.go

DESCRIPTION:
Aggregated trade: trades that fill at the same time, from the same taker
order, at the same price are aggregated into one event (Binance-style
aggTrade). Returned by the REST aggTrades endpoint and the @aggTrade stream.
*/

package types

import "github.com/shopspring/decimal"

// AggTrade — one aggregated trade.
type AggTrade struct {
	// Symbol — trading pair.
	Symbol string
	// ID — aggregate trade id (a field).
	ID int64
	// Price — trade price.
	Price decimal.Decimal
	// Quantity — trade quantity (base asset).
	Quantity decimal.Decimal
	// FirstTradeID / LastTradeID — range of raw trade ids in the aggregate.
	FirstTradeID int64
	LastTradeID  int64
	// Ts — trade time, ms.
	Ts int64
	// IsBuyerMaker — true when the buyer is the maker (price hit the bid).
	IsBuyerMaker bool
}
