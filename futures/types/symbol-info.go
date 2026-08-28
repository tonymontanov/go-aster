/*
FILE: futures/types/symbol-info.go

DESCRIPTION:
Trading rules of a single symbol, assembled from the exchangeInfo response
(symbol fields + flattened filters). The SDK flattens the filters array
(PRICE_FILTER / LOT_SIZE / MARKET_LOT_SIZE / MIN_NOTIONAL / PERCENT_PRICE /
MAX_NUM_ORDERS) into named fields so that callers do not parse filter objects.

NOTE:
PricePrecision/QuantityPrecision are DISPLAY precisions; per the Aster docs
they must NOT be used as tickSize/stepSize — use TickSize/StepSize.
*/

package types

import "github.com/shopspring/decimal"

// SymbolInfo — trading rules and precision for one symbol.
type SymbolInfo struct {
	// Symbol — trading pair (e.g. "BTCUSDT").
	Symbol string
	// Pair — underlying pair.
	Pair string
	// ContractType — PERPETUAL.
	ContractType ContractType
	// Status — trading status (TRADING etc.).
	Status ContractStatus
	// BaseAsset / QuoteAsset / MarginAsset — asset names.
	BaseAsset   string
	QuoteAsset  string
	MarginAsset string
	// PricePrecision / QuantityPrecision — display precisions (NOT tick/step).
	PricePrecision    int
	QuantityPrecision int
	// MinPrice / MaxPrice / TickSize — PRICE_FILTER.
	MinPrice decimal.Decimal
	MaxPrice decimal.Decimal
	TickSize decimal.Decimal
	// MinQuantity / MaxQuantity / StepSize — LOT_SIZE.
	MinQuantity decimal.Decimal
	MaxQuantity decimal.Decimal
	StepSize    decimal.Decimal
	// MarketMinQuantity / MarketMaxQuantity / MarketStepSize — MARKET_LOT_SIZE.
	MarketMinQuantity decimal.Decimal
	MarketMaxQuantity decimal.Decimal
	MarketStepSize    decimal.Decimal
	// MinNotional — MIN_NOTIONAL filter.
	MinNotional decimal.Decimal
	// MultiplierUp / MultiplierDown — PERCENT_PRICE bounds relative to mark price.
	MultiplierUp   decimal.Decimal
	MultiplierDown decimal.Decimal
	// MaxNumOrders — MAX_NUM_ORDERS limit (0 when the filter is absent).
	MaxNumOrders int64
	// TriggerProtect — threshold for conditional orders with priceProtect.
	TriggerProtect decimal.Decimal
	// LiquidationFee — liquidation fee rate.
	LiquidationFee decimal.Decimal
	// MarketTakeBound — max price deviation (from mark price) a market order can make.
	MarketTakeBound decimal.Decimal
	// OrderTypes — order types allowed for the symbol.
	OrderTypes []OrderType
	// TimeInForce — TIF values allowed for the symbol.
	TimeInForce []TimeInForceType
}
