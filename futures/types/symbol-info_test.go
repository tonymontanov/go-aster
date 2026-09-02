/*
FILE: futures/types/symbol-info_test.go

DESCRIPTION:
Tests for the tick/step-derived precision helpers. Values mirror the live
exchangeInfo: BSBUSDT pricePrecision=7 but tickSize=0.0000100 (5 places),
BTCUSDT tickSize=0.1 / stepSize=0.001, ASTERUSDT tickSize=0.00010.
*/

package types

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestPriceAndQuantityDecimals(t *testing.T) {
	var cases = []struct {
		name      string
		info      SymbolInfo
		wantPrice int
		wantQty   int
	}{
		{
			name: "BSBUSDT display precision exceeds tick",
			info: SymbolInfo{PricePrecision: 7, QuantityPrecision: 0,
				TickSize: decimal.RequireFromString("0.0000100"), StepSize: decimal.RequireFromString("1")},
			wantPrice: 5, wantQty: 0,
		},
		{
			name: "BTCUSDT",
			info: SymbolInfo{PricePrecision: 1, QuantityPrecision: 3,
				TickSize: decimal.RequireFromString("0.1"), StepSize: decimal.RequireFromString("0.001")},
			wantPrice: 1, wantQty: 3,
		},
		{
			name: "ASTERUSDT padded tick",
			info: SymbolInfo{PricePrecision: 5, QuantityPrecision: 2,
				TickSize: decimal.RequireFromString("0.00010"), StepSize: decimal.RequireFromString("0.01")},
			wantPrice: 4, wantQty: 2,
		},
		{
			name:      "filters absent → display precision",
			info:      SymbolInfo{PricePrecision: 4, QuantityPrecision: 2},
			wantPrice: 4, wantQty: 2,
		},
		{
			name: "integer tick",
			info: SymbolInfo{PricePrecision: 2, QuantityPrecision: 0,
				TickSize: decimal.RequireFromString("1"), StepSize: decimal.RequireFromString("10")},
			wantPrice: 0, wantQty: 0,
		},
	}
	var tc struct {
		name      string
		info      SymbolInfo
		wantPrice int
		wantQty   int
	}
	for _, tc = range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.info.PriceDecimals(); got != tc.wantPrice {
				t.Fatalf("PriceDecimals = %d, want %d", got, tc.wantPrice)
			}
			if got := tc.info.QuantityDecimals(); got != tc.wantQty {
				t.Fatalf("QuantityDecimals = %d, want %d", got, tc.wantQty)
			}
		})
	}
}
