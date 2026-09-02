/*
FILE: internal/asterr/errors_test.go

DESCRIPTION:
Tests for the Aster error-code → category mapping.
*/

package asterr

import (
	"errors"
	"fmt"
	"testing"
)

func TestMapAsterCode(t *testing.T) {
	var cases = []struct {
		code int64
		want ErrorKind
	}{
		{code: -1003, want: ErrorKindRateLimit},
		{code: -1015, want: ErrorKindRateLimit},
		{code: -1022, want: ErrorKindAuth},
		{code: -2015, want: ErrorKindAuth},
		{code: -5050, want: ErrorKindAuth}, // DEPOSIT_REQUIRED (rule since 2026-09-01)
		{code: -1007, want: ErrorKindNetwork},
		{code: -1121, want: ErrorKindInvalidRequest},
		{code: -4225, want: ErrorKindInvalidRequest},
		{code: -2019, want: ErrorKindExchange},
		{code: -9999, want: ErrorKindExchange},
		{code: 0, want: ErrorKindUnknown},
	}
	var tc struct {
		code int64
		want ErrorKind
	}
	for _, tc = range cases {
		var got ErrorKind = MapAsterCode(tc.code, "")
		if got != tc.want {
			t.Errorf("MapAsterCode(%d) = %s, want %s", tc.code, got, tc.want)
		}
	}
}

func TestIsUnknownOrder(t *testing.T) {
	var unknown error = &Error{Kind: ErrorKindExchange, AsterCode: -2011, HTTPStatus: 400, Message: "Unknown order sent."}
	var noSuch error = &Error{Kind: ErrorKindExchange, AsterCode: -2013, Message: "Order does not exist."}
	var other error = &Error{Kind: ErrorKindExchange, AsterCode: -2019, Message: "Margin is insufficient."}
	if !IsUnknownOrder(unknown) || !IsUnknownOrder(noSuch) {
		t.Fatal("-2011 / -2013 must be recognised")
	}
	if IsUnknownOrder(other) || IsUnknownOrder(nil) || IsUnknownOrder(errors.New("plain")) {
		t.Fatal("other errors must not be recognised")
	}
	// Wrapped (fmt.Errorf %w) errors are still recognised.
	if !IsUnknownOrder(fmt.Errorf("cancel: %w", unknown)) {
		t.Fatal("wrapped -2011 must be recognised")
	}
}

func TestMapHTTPStatus(t *testing.T) {
	if MapHTTPStatus(418) != ErrorKindRateLimit || MapHTTPStatus(429) != ErrorKindRateLimit {
		t.Fatal("418/429 must map to rate limit")
	}
	if MapHTTPStatus(403) != ErrorKindAuth || MapHTTPStatus(503) != ErrorKindNetwork {
		t.Fatal("403 → auth, 503 → network")
	}
}
