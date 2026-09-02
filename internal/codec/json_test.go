/*
FILE: internal/codec/json_test.go

DESCRIPTION:
Regression tests for the codec configuration. The critical property is
CASE-SENSITIVE field matching: Aster/Binance WS payloads carry keys that
differ only by case ("e"/"E", "U"/"u", "b"/"B"), and the jsoniter default
(case-insensitive) collapses them onto one struct field.
*/

package codec

import "testing"

// TestUnmarshalIsCaseSensitive — "e" (string) and "E" (int64) must land in
// different fields; the same for "U"/"u" and "b"/"B".
func TestUnmarshalIsCaseSensitive(t *testing.T) {
	var raw struct {
		EventType     string      `json:"e"`
		EventTime     int64       `json:"E"`
		FirstUpdateID int64       `json:"U"`
		FinalUpdateID int64       `json:"u"`
		BidPrice      string      `json:"b"`
		BidQty        string      `json:"B"`
		Bids          [][2]string `json:"bids"`
	}
	var payload string = `{"e":"markPriceUpdate","E":1788359866000,"U":100,"u":105,"b":"0.1","B":"25"}`
	var err error = Unmarshal([]byte(payload), &raw)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw.EventType != "markPriceUpdate" || raw.EventTime != 1788359866000 {
		t.Fatalf("e/E collision: %+v", raw)
	}
	if raw.FirstUpdateID != 100 || raw.FinalUpdateID != 105 {
		t.Fatalf("U/u collision: %+v", raw)
	}
	if raw.BidPrice != "0.1" || raw.BidQty != "25" {
		t.Fatalf("b/B collision: %+v", raw)
	}
}

// TestUnmarshalUnknownCaseVariantIsIgnored — a key that matches a field only
// case-insensitively must be IGNORED, not mapped onto that field.
func TestUnmarshalUnknownCaseVariantIsIgnored(t *testing.T) {
	var raw struct {
		Symbol string `json:"s"`
	}
	var err error = Unmarshal([]byte(`{"S":"SELL"}`), &raw)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw.Symbol != "" {
		t.Fatalf("case-insensitive match leaked: Symbol=%q", raw.Symbol)
	}
}

// TestParseHelpers — empty strings decode to zero values, malformed input
// reports an error.
func TestParseHelpers(t *testing.T) {
	var d, err = ParseDecimal("")
	if err != nil || !d.IsZero() {
		t.Fatalf("ParseDecimal(\"\") = %v, %v", d, err)
	}
	if _, err = ParseDecimal("abc"); err == nil {
		t.Fatal("ParseDecimal(\"abc\") must fail")
	}
	var n int64
	n, err = ParseInt64("")
	if err != nil || n != 0 {
		t.Fatalf("ParseInt64(\"\") = %d, %v", n, err)
	}
}
