/*
FILE: orderbook/engine_test.go

DESCRIPTION:
Unit tests of the order book engine: the Aster/Binance diff-depth
synchronization algorithm (buffering, straddle priming, pu continuity, gap
detection, stale drops), level application and depth trimming.
*/

package orderbook

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-aster/types"
)

func lvl(price, size string) types.OrderBookLevel {
	var out types.OrderBookLevel
	out.Price = decimal.RequireFromString(price)
	if size != "" {
		out.Size = decimal.RequireFromString(size)
	}
	return out
}

func snapshot(lastUpdateID int64) types.OrderBookSnapshot {
	return types.OrderBookSnapshot{
		Symbol:       "BTCUSDT",
		LastUpdateID: lastUpdateID,
		Bids:         []types.OrderBookLevel{lvl("100", "1"), lvl("99", "2")},
		Asks:         []types.OrderBookLevel{lvl("101", "1"), lvl("102", "2")},
	}
}

func delta(first, final, prevFinal int64, bids, asks []types.OrderBookLevel) Delta {
	return Delta{
		FirstUpdateID:     first,
		FinalUpdateID:     final,
		PrevFinalUpdateID: prevFinal,
		Bids:              bids,
		Asks:              asks,
	}
}

func TestEngineBuffersUntilSnapshot(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 0)

	// Deltas before the snapshot must be buffered, not applied.
	var res ApplyResult = e.ApplyDelta(delta(90, 95, 89, []types.OrderBookLevel{lvl("100", "5")}, nil))
	if res.Gap != GapNotPrimed {
		t.Fatalf("expected GapNotPrimed, got %v", res.Gap)
	}
	// Straddling delta buffered too.
	_ = e.ApplyDelta(delta(96, 105, 95, []types.OrderBookLevel{lvl("100", "7")}, nil))
	// Snapshot at 100: first buffered delta is stale (u=95 < 100), the second
	// straddles (U=96 <= 100 <= u=105) and must be applied.
	res = e.ApplySnapshot(snapshot(100))
	if res.Gap != GapNone {
		t.Fatalf("expected GapNone after snapshot replay, got %v", res.Gap)
	}
	if e.LastUpdateID() != 105 {
		t.Fatalf("lastUpdateID = %d, want 105", e.LastUpdateID())
	}
	var bidPx, bidSz, _, _ = e.BestBidAsk()
	if !bidPx.Equal(decimal.RequireFromString("100")) || !bidSz.Equal(decimal.RequireFromString("7")) {
		t.Fatalf("best bid = %s@%s, want 7@100", bidSz, bidPx)
	}
}

func TestEngineSequenceContinuity(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 0)
	_ = e.ApplySnapshot(snapshot(100))

	// First delta must straddle lastUpdateId.
	var res ApplyResult = e.ApplyDelta(delta(98, 110, 97, nil, []types.OrderBookLevel{lvl("101", "9")}))
	if res.Gap != GapNone || !res.Applied {
		t.Fatalf("straddling delta not applied: %+v", res)
	}
	// Continuation: pu must equal previous u.
	res = e.ApplyDelta(delta(111, 120, 110, []types.OrderBookLevel{lvl("99", "0")}, nil))
	if res.Gap != GapNone || !res.Applied {
		t.Fatalf("continuous delta not applied: %+v", res)
	}
	// Level 99 removed by zero size.
	var bids, _ = e.TopLevels(0)
	if len(bids) != 1 {
		t.Fatalf("bids len = %d, want 1 (99 removed)", len(bids))
	}
}

func TestEngineGapDetection(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 0)
	_ = e.ApplySnapshot(snapshot(100))
	_ = e.ApplyDelta(delta(98, 110, 97, nil, nil))

	// pu=115 != last u=110 → gap.
	var res ApplyResult = e.ApplyDelta(delta(116, 120, 115, nil, nil))
	if res.Gap != GapSequence {
		t.Fatalf("expected GapSequence, got %v", res.Gap)
	}
	if !e.IsDirty() {
		t.Fatal("engine must be dirty after a gap")
	}
	// While dirty, deltas keep buffering and reporting the gap.
	res = e.ApplyDelta(delta(121, 125, 120, nil, nil))
	if res.Gap != GapSequence {
		t.Fatalf("expected GapSequence while dirty, got %v", res.Gap)
	}
	// A newer snapshot heals: buffered 116-120 and 121-125 replay on top of
	// lastUpdateId=118 (drop-stale + straddle + continuity).
	var snap types.OrderBookSnapshot = snapshot(118)
	res = e.ApplySnapshot(snap)
	if res.Gap != GapNone {
		t.Fatalf("expected recovery after newer snapshot, got %v", res.Gap)
	}
	if e.LastUpdateID() != 125 {
		t.Fatalf("lastUpdateID = %d, want 125", e.LastUpdateID())
	}
}

func TestEngineStaleDeltasDropped(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 0)
	_ = e.ApplySnapshot(snapshot(100))

	var res ApplyResult = e.ApplyDelta(delta(80, 90, 79, []types.OrderBookLevel{lvl("50", "1")}, nil))
	if res.Gap != GapNone || !res.Stale || res.Applied {
		t.Fatalf("stale delta must be dropped: %+v", res)
	}
	var bids, _ = e.TopLevels(0)
	if len(bids) != 2 {
		t.Fatalf("stale delta modified the book")
	}
}

func TestEngineSnapshotBehindStreamRequestsResync(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 0)
	// Stream is far ahead of the snapshot: U=200 > lastUpdateId=100.
	_ = e.ApplyDelta(delta(200, 210, 199, nil, nil))
	var res ApplyResult = e.ApplySnapshot(snapshot(100))
	if res.Gap != GapSequence {
		t.Fatalf("expected GapSequence when snapshot is behind the stream, got %v", res.Gap)
	}
	// Newer snapshot inside the buffered window recovers.
	res = e.ApplySnapshot(snapshot(205))
	if res.Gap != GapNone {
		t.Fatalf("expected recovery, got %v", res.Gap)
	}
	if e.LastUpdateID() != 210 {
		t.Fatalf("lastUpdateID = %d, want 210", e.LastUpdateID())
	}
}

func TestEngineLevelOrderingAndTrim(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 2)
	_ = e.ApplySnapshot(types.OrderBookSnapshot{
		Symbol:       "BTCUSDT",
		LastUpdateID: 1,
		Bids:         []types.OrderBookLevel{lvl("100", "1"), lvl("99", "1")},
		Asks:         []types.OrderBookLevel{lvl("101", "1"), lvl("102", "1")},
	})
	// Insert a better bid and a worse ask; maxDepth=2 must trim tails.
	_ = e.ApplyDelta(delta(1, 2, 1, []types.OrderBookLevel{lvl("100.5", "3")}, []types.OrderBookLevel{lvl("103", "3")}))

	var bids, asks = e.TopLevels(0)
	if len(bids) != 2 || len(asks) != 2 {
		t.Fatalf("trim failed: bids=%d asks=%d, want 2/2", len(bids), len(asks))
	}
	if !bids[0].Price.Equal(decimal.RequireFromString("100.5")) {
		t.Fatalf("best bid = %s, want 100.5", bids[0].Price)
	}
	if !bids[0].Price.GreaterThan(bids[1].Price) {
		t.Fatal("bids must be sorted descending")
	}
	if !asks[0].Price.LessThan(asks[1].Price) {
		t.Fatal("asks must be sorted ascending")
	}
}

func TestEngineMarkResynced(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 0)
	_ = e.ApplySnapshot(snapshot(100))
	_ = e.ApplyDelta(delta(98, 110, 97, nil, nil))

	e.MarkResynced()
	if e.IsDirty() || e.LastUpdateID() != 0 {
		t.Fatal("MarkResynced must fully reset the engine")
	}
	var bids, asks = e.TopLevels(0)
	if len(bids) != 0 || len(asks) != 0 {
		t.Fatal("MarkResynced must clear the book")
	}
	// After reset the engine behaves like a fresh one.
	var res ApplyResult = e.ApplyDelta(delta(300, 310, 299, nil, nil))
	if res.Gap != GapNotPrimed {
		t.Fatalf("expected GapNotPrimed after reset, got %v", res.Gap)
	}
}

func TestEngineSnapshotConsistency(t *testing.T) {
	var e *Engine = NewEngine("BTCUSDT", 0)
	_ = e.ApplySnapshot(snapshot(100))

	var snap types.OrderBookSnapshot = e.Snapshot(1)
	if snap.Symbol != "BTCUSDT" || snap.LastUpdateID != 100 {
		t.Fatalf("snapshot metadata wrong: %+v", snap)
	}
	if len(snap.Bids) != 1 || len(snap.Asks) != 1 {
		t.Fatalf("depth clamp failed: %d/%d", len(snap.Bids), len(snap.Asks))
	}
	// Returned slices must be copies: mutating them must not affect the book.
	snap.Bids[0].Size = decimal.RequireFromString("999")
	var bids, _ = e.TopLevels(0)
	if bids[0].Size.Equal(decimal.RequireFromString("999")) {
		t.Fatal("Snapshot must return copies, not internal slices")
	}
}
