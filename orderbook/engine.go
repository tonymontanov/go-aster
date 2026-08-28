/*
FILE: orderbook/engine.go

DESCRIPTION:
Local L2 order book engine for Aster diff-depth streams (Binance-style
sequencing). One Engine instance serves one symbol.

SYNCHRONIZATION ALGORITHM (per the Aster docs, identical to Binance USD-M):
 1. Subscribe to <symbol>@depth and BUFFER incoming events.
 2. Fetch a REST snapshot (lastUpdateId).
 3. Drop buffered events with u < lastUpdateId.
 4. The first applied event must satisfy U <= lastUpdateId AND u >= lastUpdateId.
 5. Every subsequent event must satisfy pu == previous event's u; otherwise
    the book is out of sync — refetch the snapshot and start over.
 6. Level quantities are ABSOLUTE; quantity 0 removes the level. Removals of
    unknown levels are normal.

The engine implements steps 3-6 plus the pre-snapshot buffering of step 1:
deltas arriving before ApplySnapshot are queued (bounded) and replayed on
snapshot application. The caller (futures.StreamClient) owns step 2 — it
fetches REST snapshots whenever an ApplyResult requests one.

CONCURRENCY:
All methods are safe for concurrent use (single RWMutex). In practice deltas
arrive from one read loop and snapshots from one resync goroutine.

PERFORMANCE:
Levels are kept in sorted slices (bids desc, asks asc) with binary-search
insert/delete — O(log n) search + memmove, allocation-free in steady state.
*/

package orderbook

import (
	"sort"
	"sync"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-aster/types"
)

// GapKind — synchronization failure classification.
type GapKind uint8

const (
	// GapNone — delta applied (or harmlessly dropped as stale).
	GapNone GapKind = iota
	// GapNotPrimed — no snapshot yet: the delta was buffered; the caller
	// should ensure a snapshot fetch is in flight.
	GapNotPrimed
	// GapSequence — sequence broken (pu != last u, or the buffered window
	// does not straddle the snapshot): the book is dirty, a fresh snapshot
	// is required.
	GapSequence
)

// maxPendingDeltas bounds the pre-snapshot buffer. At the fastest stream
// speed (100ms) this covers well over a minute of buffering — if a snapshot
// cannot be fetched in that time the stream is unhealthy anyway and a resync
// restarts the process.
const maxPendingDeltas = 1024

// Delta — one diff-depth event.
type Delta struct {
	// FirstUpdateID — U field (first update id in event).
	FirstUpdateID int64
	// FinalUpdateID — u field (final update id in event).
	FinalUpdateID int64
	// PrevFinalUpdateID — pu field (final update id of the previous event).
	PrevFinalUpdateID int64
	// EventTs / TxTs — event and transaction times, ms.
	EventTs int64
	TxTs    int64
	// Bids / Asks — absolute-quantity level updates.
	Bids []types.OrderBookLevel
	Asks []types.OrderBookLevel
}

// ApplyResult — outcome of ApplySnapshot/ApplyDelta.
type ApplyResult struct {
	// Gap — synchronization state after the call.
	Gap GapKind
	// Applied — true when the book content changed.
	Applied bool
	// Stale — true when the delta was older than the snapshot and dropped.
	Stale bool
	// LastUpdateID — current book sequence anchor.
	LastUpdateID int64
	// BidsLen / AsksLen — current book sizes (diagnostics).
	BidsLen int
	AsksLen int
}

// Engine — local order book for one symbol.
type Engine struct {
	symbol   string
	maxDepth int

	mu           sync.RWMutex
	bids         []types.OrderBookLevel // sorted desc by price
	asks         []types.OrderBookLevel // sorted asc by price
	lastUpdateID int64
	primed       bool // snapshot applied
	synced       bool // first straddling delta applied after snapshot
	dirty        bool // resync required
	pending      []Delta
	lastEventTs  int64
}

// NewEngine creates an engine for one symbol. maxDepth bounds the number of
// levels kept per side (0 = unlimited).
func NewEngine(symbol string, maxDepth int) *Engine {
	return &Engine{
		symbol:   symbol,
		maxDepth: maxDepth,
	}
}

// Symbol returns the engine's symbol.
func (e *Engine) Symbol() string { return e.symbol }

// IsDirty returns true when the book needs a resync (fresh snapshot).
func (e *Engine) IsDirty() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dirty
}

// LastUpdateID returns the current sequence anchor.
func (e *Engine) LastUpdateID() int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastUpdateID
}

// LastEventTs returns the event time (ms) of the last applied update.
func (e *Engine) LastEventTs() int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastEventTs
}

// MarkResynced drops ALL state (book, buffer, sequence). Called on WS
// reconnect (Subscription.Reset) so that a new socket starts from scratch.
func (e *Engine) MarkResynced() {
	e.mu.Lock()
	e.bids = e.bids[:0]
	e.asks = e.asks[:0]
	e.lastUpdateID = 0
	e.primed = false
	e.synced = false
	e.dirty = false
	e.pending = e.pending[:0]
	e.lastEventTs = 0
	e.mu.Unlock()
}

/*
ApplySnapshot initializes the book from a REST snapshot and replays buffered
deltas (steps 3-5 of the sync algorithm). Returns GapSequence when the replay
detects a broken sequence — the caller must fetch a NEWER snapshot (the buffer
is preserved only for deltas ahead of the snapshot).
*/
func (e *Engine) ApplySnapshot(snap types.OrderBookSnapshot) ApplyResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.bids = append(e.bids[:0], snap.Bids...)
	e.asks = append(e.asks[:0], snap.Asks...)
	e.lastUpdateID = snap.LastUpdateID
	e.primed = true
	e.synced = false
	e.dirty = false
	e.lastEventTs = snap.EventTs

	// Replay the buffered deltas accumulated while the snapshot was fetched.
	var pending []Delta = e.pending
	e.pending = nil
	var i int
	for i = 0; i < len(pending); i++ {
		if e.dirty {
			// A replayed delta broke the sequence: keep the remaining tail
			// buffered so the NEXT snapshot can replay it.
			e.bufferLocked(pending[i])
			continue
		}
		_ = e.applyDeltaLocked(pending[i])
	}
	if e.dirty {
		return e.resultLocked(GapSequence)
	}
	return e.resultLocked(GapNone)
}

/*
ApplyDelta processes one diff-depth event.

Outcomes:
  - GapNotPrimed: no snapshot yet — the delta was buffered; ensure a snapshot
    fetch is in flight;
  - Stale: delta entirely below the snapshot — dropped, book unchanged;
  - GapSequence: sequence broken — the book is dirty until a fresh snapshot;
  - GapNone + Applied: delta applied.
*/
func (e *Engine) ApplyDelta(d Delta) ApplyResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.dirty {
		// Waiting for a fresh snapshot: buffer so the resync can replay.
		e.bufferLocked(d)
		return e.resultLocked(GapSequence)
	}
	if !e.primed {
		e.bufferLocked(d)
		return e.resultLocked(GapNotPrimed)
	}
	return e.applyDeltaLocked(d)
}

// bufferLocked queues a pre-snapshot delta (bounded).
func (e *Engine) bufferLocked(d Delta) {
	if len(e.pending) >= maxPendingDeltas {
		// Overflow: drop the oldest half to keep the newest window. The sync
		// algorithm only needs deltas around the upcoming snapshot's
		// lastUpdateId, which are the newest ones.
		copy(e.pending, e.pending[len(e.pending)/2:])
		e.pending = e.pending[:len(e.pending)-len(e.pending)/2]
	}
	e.pending = append(e.pending, d)
}

// applyDeltaLocked applies one delta to a primed book.
func (e *Engine) applyDeltaLocked(d Delta) ApplyResult {
	// Step 3: drop events entirely below the snapshot.
	if d.FinalUpdateID < e.lastUpdateID {
		var res ApplyResult = e.resultLocked(GapNone)
		res.Stale = true
		return res
	}

	if !e.synced {
		// Step 4: the first applied event must straddle lastUpdateId.
		if d.FirstUpdateID > e.lastUpdateID {
			// The snapshot is older than the delta window start — resync.
			e.dirty = true
			e.bufferLocked(d)
			return e.resultLocked(GapSequence)
		}
		e.synced = true
	} else if d.PrevFinalUpdateID != e.lastUpdateID {
		// Step 5: pu must equal the previous event's u.
		e.dirty = true
		e.bufferLocked(d)
		return e.resultLocked(GapSequence)
	}

	var i int
	for i = 0; i < len(d.Bids); i++ {
		e.bids = applyLevel(e.bids, d.Bids[i], false)
	}
	for i = 0; i < len(d.Asks); i++ {
		e.asks = applyLevel(e.asks, d.Asks[i], true)
	}
	e.trimLocked()
	e.lastUpdateID = d.FinalUpdateID
	e.lastEventTs = d.EventTs

	var res ApplyResult = e.resultLocked(GapNone)
	res.Applied = true
	return res
}

// resultLocked builds an ApplyResult snapshot of the current state.
func (e *Engine) resultLocked(gap GapKind) ApplyResult {
	return ApplyResult{
		Gap:          gap,
		LastUpdateID: e.lastUpdateID,
		BidsLen:      len(e.bids),
		AsksLen:      len(e.asks),
	}
}

// trimLocked bounds both sides to maxDepth levels.
func (e *Engine) trimLocked() {
	if e.maxDepth <= 0 {
		return
	}
	if len(e.bids) > e.maxDepth {
		e.bids = e.bids[:e.maxDepth]
	}
	if len(e.asks) > e.maxDepth {
		e.asks = e.asks[:e.maxDepth]
	}
}

/*
applyLevel inserts/updates/removes one level in a sorted side.
asc=true for asks (ascending prices), false for bids (descending).
Quantities are absolute; zero removes the level.
*/
func applyLevel(side []types.OrderBookLevel, lvl types.OrderBookLevel, asc bool) []types.OrderBookLevel {
	var less func(a, b decimal.Decimal) bool
	if asc {
		less = func(a, b decimal.Decimal) bool { return a.LessThan(b) }
	} else {
		less = func(a, b decimal.Decimal) bool { return a.GreaterThan(b) }
	}

	var idx int = sort.Search(len(side), func(i int) bool {
		return !less(side[i].Price, lvl.Price)
	})

	if idx < len(side) && side[idx].Price.Equal(lvl.Price) {
		if lvl.Size.IsZero() {
			side = append(side[:idx], side[idx+1:]...)
		} else {
			side[idx].Size = lvl.Size
		}
		return side
	}
	if lvl.Size.IsZero() {
		// Removal of an unknown level — normal per the protocol.
		return side
	}
	side = append(side, types.OrderBookLevel{})
	copy(side[idx+1:], side[idx:])
	side[idx] = lvl
	return side
}

// TopLevels returns copies of the top n levels of both sides (bids desc,
// asks asc). n <= 0 returns full depth.
func (e *Engine) TopLevels(n int) ([]types.OrderBookLevel, []types.OrderBookLevel) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var nb int = len(e.bids)
	var na int = len(e.asks)
	if n > 0 {
		if nb > n {
			nb = n
		}
		if na > n {
			na = n
		}
	}
	var bids []types.OrderBookLevel = make([]types.OrderBookLevel, nb)
	var asks []types.OrderBookLevel = make([]types.OrderBookLevel, na)
	copy(bids, e.bids[:nb])
	copy(asks, e.asks[:na])
	return bids, asks
}

// Snapshot returns a consistent snapshot of the top `depth` levels plus
// sequence/timestamp metadata (one lock acquisition). depth <= 0 — full book.
func (e *Engine) Snapshot(depth int) types.OrderBookSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var nb int = len(e.bids)
	var na int = len(e.asks)
	if depth > 0 {
		if nb > depth {
			nb = depth
		}
		if na > depth {
			na = depth
		}
	}
	var out types.OrderBookSnapshot = types.OrderBookSnapshot{
		Symbol:       e.symbol,
		Bids:         make([]types.OrderBookLevel, nb),
		Asks:         make([]types.OrderBookLevel, na),
		LastUpdateID: e.lastUpdateID,
		EventTs:      e.lastEventTs,
	}
	copy(out.Bids, e.bids[:nb])
	copy(out.Asks, e.asks[:na])
	return out
}

// BestBidAsk returns the best bid/ask prices and sizes. Zeros when a side is
// empty.
func (e *Engine) BestBidAsk() (bidPx, bidSz, askPx, askSz decimal.Decimal) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.bids) > 0 {
		bidPx = e.bids[0].Price
		bidSz = e.bids[0].Size
	}
	if len(e.asks) > 0 {
		askPx = e.asks[0].Price
		askSz = e.asks[0].Size
	}
	return bidPx, bidSz, askPx, askSz
}
