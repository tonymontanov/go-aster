/*
FILE: internal/asterr/errors.go

DESCRIPTION:
SDK error type + categories + Aster code mapping. Placed in an internal package
so that any internal/* package (rest, ws, codec) can use it without an import
cycle on the root aster package. The root aster package re-exports these
entities via type aliases.

ASTER SPECIFICS:
Aster error codes follow the Binance Futures convention: negative integers in a
`{"code": -1121, "msg": "Invalid symbol."}` payload. Codes are grouped:
  - 10xx — general server or network issues;
  - 11xx — request issues (validation);
  - 20xx — processing issues (order rejected, balance, etc.);
  - 40xx — filters and other issues;
  - 42xx — V3-specific (e.g. -4225 Nonce Expired).

See also: errors.go in the root (re-export).
*/

package asterr

import (
	"errors"
	"fmt"
)

// ErrorKind — SDK error category.
type ErrorKind uint8

const (
	ErrorKindUnknown ErrorKind = iota
	ErrorKindNetwork
	ErrorKindRateLimit
	ErrorKindAuth
	ErrorKindInvalidRequest
	ErrorKindExchange
)

// String — human-readable category name.
func (k ErrorKind) String() string {
	switch k {
	case ErrorKindNetwork:
		return "network"
	case ErrorKindRateLimit:
		return "rate_limit"
	case ErrorKindAuth:
		return "auth"
	case ErrorKindInvalidRequest:
		return "invalid_request"
	case ErrorKindExchange:
		return "exchange"
	default:
		return "unknown"
	}
}

// Error — unified SDK error type.
type Error struct {
	Kind       ErrorKind
	HTTPStatus int
	// AsterCode — exchange error code (negative integer, Binance-style).
	// 0 means "no exchange code" (e.g. transport errors).
	AsterCode int64
	Message   string
	Cause     error
}

// Error implements the error interface.
func (e *Error) Error() string {
	switch {
	case e.AsterCode != 0 && e.Cause != nil:
		return fmt.Sprintf("aster %s: code=%d status=%d msg=%q: %v", e.Kind, e.AsterCode, e.HTTPStatus, e.Message, e.Cause)
	case e.AsterCode != 0:
		return fmt.Sprintf("aster %s: code=%d status=%d msg=%q", e.Kind, e.AsterCode, e.HTTPStatus, e.Message)
	case e.Cause != nil:
		return fmt.Sprintf("aster %s: status=%d msg=%q: %v", e.Kind, e.HTTPStatus, e.Message, e.Cause)
	default:
		return fmt.Sprintf("aster %s: status=%d msg=%q", e.Kind, e.HTTPStatus, e.Message)
	}
}

// Unwrap — for errors.Is/As.
func (e *Error) Unwrap() error { return e.Cause }

// New creates a *Error.
func New(kind ErrorKind, code int64, msg string, cause error) *Error {
	return &Error{Kind: kind, AsterCode: code, Message: msg, Cause: cause}
}

// IsNetwork returns true if err has category Network.
func IsNetwork(err error) bool { return matchKind(err, ErrorKindNetwork) }

// IsRateLimit returns true if err has category RateLimit.
func IsRateLimit(err error) bool { return matchKind(err, ErrorKindRateLimit) }

// IsAuth returns true if err has category Auth.
func IsAuth(err error) bool { return matchKind(err, ErrorKindAuth) }

// IsInvalidRequest returns true if err has category InvalidRequest.
func IsInvalidRequest(err error) bool { return matchKind(err, ErrorKindInvalidRequest) }

// IsExchange returns true if err has category Exchange.
func IsExchange(err error) bool { return matchKind(err, ErrorKindExchange) }

func matchKind(err error, kind ErrorKind) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind == kind
	}
	return false
}

/*
MapAsterCode returns the SDK error category for a specific Aster error code.

Covered groups (codes follow the Binance Futures convention):
  - -1003 TOO_MANY_REQUESTS, -1015 TOO_MANY_ORDERS         — rate limit;
  - -1002 UNAUTHORIZED, -1022 INVALID_SIGNATURE,
    -2014 BAD_API_KEY_FMT, -2015 REJECTED_MBX_KEY          — authentication;
  - -1001 DISCONNECTED, -1006 UNEXPECTED_RESP, -1007 TIMEOUT,
    -1016 SERVICE_SHUTTING_DOWN                            — network/server side;
  - -11xx (params), -1013/-1014/-1020/-1023,
    -40xx filters, -4225 Nonce Expired                     — invalid request;
  - -2010..-2028 (order/balance/margin rejections)         — exchange;
  - everything else                                        — ErrorKindExchange.
*/
func MapAsterCode(code int64, msg string) ErrorKind {
	_ = msg
	switch {
	case code == 0:
		return ErrorKindUnknown
	case code == -1003 || code == -1015:
		return ErrorKindRateLimit
	case code == -1002 || code == -1022 || code == -2014 || code == -2015:
		return ErrorKindAuth
	case code == -1001 || code == -1006 || code == -1007 || code == -1016:
		return ErrorKindNetwork
	case code == -1013 || code == -1014 || code == -1020 || code == -1023:
		return ErrorKindInvalidRequest
	case code <= -1100 && code > -1200:
		return ErrorKindInvalidRequest
	case code <= -4000 && code > -4300:
		return ErrorKindInvalidRequest
	case code <= -2010 && code > -2100:
		return ErrorKindExchange
	default:
		return ErrorKindExchange
	}
}

// MapHTTPStatus returns the SDK error category for an HTTP status code (when
// the response body is invalid or does not contain an Aster code).
// 418 is the Aster/Binance "IP auto-ban" status — mapped to RateLimit.
func MapHTTPStatus(status int) ErrorKind {
	switch {
	case status == 429 || status == 418:
		return ErrorKindRateLimit
	case status == 401 || status == 403:
		return ErrorKindAuth
	case status >= 500:
		return ErrorKindNetwork
	case status >= 400:
		return ErrorKindInvalidRequest
	default:
		return ErrorKindUnknown
	}
}
