/*
FILE: futures/types/mini-ticker.go

DESCRIPTION:
24hr rolling window mini-ticker (@miniTicker stream). The Close field is the
latest price — the stream serves as a last-price feed.
*/

package types

import "github.com/shopspring/decimal"

// MiniTicker — one @miniTicker event.
type MiniTicker struct {
	// Symbol — trading pair.
	Symbol string
	// EventTs — event time, ms.
	EventTs int64
	// Close — latest price.
	Close decimal.Decimal
	// Open / High / Low — 24h window prices.
	Open decimal.Decimal
	High decimal.Decimal
	Low  decimal.Decimal
	// Volume / QuoteVolume — 24h volumes.
	Volume      decimal.Decimal
	QuoteVolume decimal.Decimal
}
