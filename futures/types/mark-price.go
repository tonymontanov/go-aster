/*
FILE: futures/types/mark-price.go

DESCRIPTION:
Mark price / funding data for one symbol. Returned by GET /fapi/v3/premiumIndex
and emitted by the @markPrice stream (WatchMarkPrice).
*/

package types

import "github.com/shopspring/decimal"

// MarkPriceInfo — mark price and funding state of one symbol.
type MarkPriceInfo struct {
	// Symbol — trading pair.
	Symbol string
	// MarkPrice — current mark price.
	MarkPrice decimal.Decimal
	// IndexPrice — underlying index price.
	IndexPrice decimal.Decimal
	// EstimatedSettlePrice — estimated settle price (meaningful only in the
	// last hour before settlement).
	EstimatedSettlePrice decimal.Decimal
	// LastFundingRate — latest funding rate.
	LastFundingRate decimal.Decimal
	// NextFundingTime — next funding time, ms.
	NextFundingTime int64
	// InterestRate — interest rate.
	InterestRate decimal.Decimal
	// Ts — event/response time, ms.
	Ts int64
}

// FundingRateEntry — one historical funding rate record
// (GET /fapi/v3/fundingRate).
type FundingRateEntry struct {
	// Symbol — trading pair.
	Symbol string
	// FundingRate — funding rate of the interval.
	FundingRate decimal.Decimal
	// FundingTime — funding time, ms.
	FundingTime int64
}
