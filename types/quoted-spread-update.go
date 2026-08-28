/*
FILE: types/quoted-spread-update.go

DESCRIPTION:
Best bid/ask (BBO) update. Emitted by WatchSpread from the @bookTicker stream
and available via the REST bookTicker endpoint.
*/

package types

import "github.com/shopspring/decimal"

// QuotedSpreadUpdate — best bid/offer update for a single symbol.
type QuotedSpreadUpdate struct {
	// Symbol — trading pair.
	Symbol string
	// UpdateID — order book update id of this event (u field).
	UpdateID int64
	// BestBidPrice / BestBidQty — best bid level.
	BestBidPrice decimal.Decimal
	BestBidQty   decimal.Decimal
	// BestAskPrice / BestAskQty — best ask level.
	BestAskPrice decimal.Decimal
	BestAskQty   decimal.Decimal
	// EventTs — event time, ms (0 in REST responses).
	EventTs int64
	// TxTs — transaction/matching time, ms (0 if absent).
	TxTs int64
}
