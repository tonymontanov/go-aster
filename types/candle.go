/*
FILE: types/candle.go

DESCRIPTION:
A single kline/candlestick and the Candles collection. Returned by
GetHistoricalCandles (REST klines) and emitted by WatchKline.

The wire format is a positional JSON array:

	[ openTime, "open", "high", "low", "close", "volume", closeTime,
	  "quoteVolume", trades, "takerBuyBaseVolume", "takerBuyQuoteVolume", "ignore" ]

The SDK decodes it into this named struct at the domain layer.
*/

package types

import "github.com/shopspring/decimal"

// Candle — one kline/candlestick.
type Candle struct {
	// OpenTime — candle open time, ms.
	OpenTime int64
	// CloseTime — candle close time, ms.
	CloseTime int64
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	// Volume — base asset volume.
	Volume decimal.Decimal
	// QuoteVolume — quote asset volume.
	QuoteVolume decimal.Decimal
	// Trades — number of trades in the candle.
	Trades int64
	// TakerBuyBaseVolume — taker buy volume in base asset.
	TakerBuyBaseVolume decimal.Decimal
	// TakerBuyQuoteVolume — taker buy volume in quote asset.
	TakerBuyQuoteVolume decimal.Decimal
}

// Candles — chronologically ascending candle series (oldest first, matching
// the exchange delivery order).
type Candles []Candle
