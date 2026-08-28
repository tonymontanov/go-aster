/*
FILE: types/order-book-snapshot.go

DESCRIPTION:
Order book snapshot: a set of bid/ask levels plus lastUpdateId and timestamp
metadata. Returned by the GetOrderBook REST endpoint, used as input to
orderbook.Engine.ApplySnapshot, and emitted to WatchOrderbook subscribers.

The format is identical for futures and spot (Binance-style depth payload).

FIELDS:
  - Symbol       — trading pair.
  - Bids         — buy levels, sorted in descending price order.
  - Asks         — sell levels, sorted in ascending price order.
  - LastUpdateID — the exchange lastUpdateId; anchor for the diff-depth
                   synchronization algorithm (U <= lastUpdateId <= u).
  - EventTs      — event time in ms (E field; 0 in pure REST snapshots).
  - TxTs         — transaction/matching time in ms (T field; 0 if absent).
*/

package types

// OrderBookSnapshot — order book snapshot for a single symbol.
type OrderBookSnapshot struct {
	Symbol       string
	Bids         []OrderBookLevel
	Asks         []OrderBookLevel
	LastUpdateID int64
	EventTs      int64
	TxTs         int64
}
