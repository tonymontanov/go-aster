/*
FILE: errors.go

DESCRIPTION:
Public re-export of entities from the internal/asterr package. The type itself,
categories, and mappings live in internal/asterr (see documentation there);
here — only the type alias and function variables so that the user works through
the familiar root-package import:

    import aster "github.com/tonymontanov/go-aster"

    if aster.IsRateLimit(err) { ... }
*/

package aster

import "github.com/tonymontanov/go-aster/internal/asterr"

// Error — SDK error type. Alias.
type Error = asterr.Error

// ErrorKind — SDK error category. Alias.
type ErrorKind = asterr.ErrorKind

// Categories. Declared as typed constants via alias.
const (
	ErrorKindUnknown        = asterr.ErrorKindUnknown
	ErrorKindNetwork        = asterr.ErrorKindNetwork
	ErrorKindRateLimit      = asterr.ErrorKindRateLimit
	ErrorKindAuth           = asterr.ErrorKindAuth
	ErrorKindInvalidRequest = asterr.ErrorKindInvalidRequest
	ErrorKindExchange       = asterr.ErrorKindExchange
)

// NewError creates a *Error.
func NewError(kind ErrorKind, code int64, msg string, cause error) *Error {
	return asterr.New(kind, code, msg, cause)
}

// IsNetwork / IsRateLimit / IsAuth / IsInvalidRequest / IsExchange — error
// category predicates.
func IsNetwork(err error) bool        { return asterr.IsNetwork(err) }
func IsRateLimit(err error) bool      { return asterr.IsRateLimit(err) }
func IsAuth(err error) bool           { return asterr.IsAuth(err) }
func IsInvalidRequest(err error) bool { return asterr.IsInvalidRequest(err) }
func IsExchange(err error) bool       { return asterr.IsExchange(err) }

// IsUnknownOrder reports whether err carries the -2011 UNKNOWN_ORDER or
// -2013 NO_SUCH_ORDER exchange code: the order is already gone (filled,
// cancelled or never accepted). Cancel callers usually treat it as success.
func IsUnknownOrder(err error) bool { return asterr.IsUnknownOrder(err) }

// MapAsterCode returns the SDK error category for an exchange code.
func MapAsterCode(code int64, msg string) ErrorKind { return asterr.MapAsterCode(code, msg) }

// MapHTTPStatus returns the SDK error category for an HTTP status code.
func MapHTTPStatus(status int) ErrorKind { return asterr.MapHTTPStatus(status) }
