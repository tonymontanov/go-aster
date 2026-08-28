/*
FILE: types/kline-update.go

DESCRIPTION:
Streaming kline update (@kline_<interval> streams). Carries the in-progress
candle plus the closed flag; a candle is emitted repeatedly while open and one
final time with IsClosed=true.
*/

package types

// KlineUpdate — one streaming kline event.
type KlineUpdate struct {
	// Symbol — trading pair.
	Symbol string
	// Timeframe — kline interval.
	Timeframe Timeframe
	// Candle — current candle state.
	Candle Candle
	// IsClosed — true when this event finalizes the candle.
	IsClosed bool
	// EventTs — event time, ms.
	EventTs int64
}
