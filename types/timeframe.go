/*
FILE: types/timeframe.go

DESCRIPTION:
Candle timeframe enum matching the Aster kline interval notation
(m = minutes, h = hours, d = days, w = weeks, M = months).

MAIN FUNCTIONS:
  - ValidateTimeframe: checks that a value is one of the intervals accepted by
    the exchange (used by GetHistoricalCandles before hitting the network).
*/

package types

// Timeframe — kline/candlestick interval in exchange notation.
type Timeframe string

const (
	Timeframe1m  Timeframe = "1m"
	Timeframe3m  Timeframe = "3m"
	Timeframe5m  Timeframe = "5m"
	Timeframe15m Timeframe = "15m"
	Timeframe30m Timeframe = "30m"
	Timeframe1h  Timeframe = "1h"
	Timeframe2h  Timeframe = "2h"
	Timeframe4h  Timeframe = "4h"
	Timeframe6h  Timeframe = "6h"
	Timeframe8h  Timeframe = "8h"
	Timeframe12h Timeframe = "12h"
	Timeframe1d  Timeframe = "1d"
	Timeframe3d  Timeframe = "3d"
	Timeframe1w  Timeframe = "1w"
	Timeframe1M  Timeframe = "1M"
)

// ValidateTimeframe returns true when tf is an interval accepted by the
// exchange kline endpoints and streams.
func ValidateTimeframe(tf Timeframe) bool {
	switch tf {
	case Timeframe1m, Timeframe3m, Timeframe5m, Timeframe15m, Timeframe30m,
		Timeframe1h, Timeframe2h, Timeframe4h, Timeframe6h, Timeframe8h,
		Timeframe12h, Timeframe1d, Timeframe3d, Timeframe1w, Timeframe1M:
		return true
	default:
		return false
	}
}
