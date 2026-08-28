/*
FILE: types/order-book-level.go

DESCRIPTION:
A single order book level (one depth point: price + size). Used in:
  - REST snapshot (GetOrderBook);
  - orderbook engine (snapshot + diff);
  - WS push (WatchOrderbook).

The level format is identical for futures and spot in the Aster protocol
(a ["price", "qty"] string pair on the wire).

FIELDS:
  - Price — level price. decimal.Decimal — lossless and comparable without
    epsilon tricks.
  - Size  — volume at this level in base asset units. Zero size on a diff
    means "remove the level".
*/

package types

import "github.com/shopspring/decimal"

// OrderBookLevel — one order book level.
type OrderBookLevel struct {
	Price decimal.Decimal
	Size  decimal.Decimal
}
