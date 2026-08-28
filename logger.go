/*
FILE: logger.go

DESCRIPTION:
Public re-export of the Logger interface and typed Field factories. The
interface/type itself lives in internal/asterlog (see documentation there);
here — type aliases and thin wrapper functions.
*/

package aster

import "github.com/tonymontanov/go-aster/internal/asterlog"

// Logger — SDK logging interface. Alias.
type Logger = asterlog.Logger

// Field — typed log field. Alias.
type Field = asterlog.Field

// FieldKind — Field discriminator. Alias.
type FieldKind = asterlog.FieldKind

// FieldKind values.
const (
	FieldKindString = asterlog.FieldKindString
	FieldKindInt    = asterlog.FieldKindInt
	FieldKindFloat  = asterlog.FieldKindFloat
	FieldKindBool   = asterlog.FieldKindBool
	FieldKindError  = asterlog.FieldKindError
)

// NoopLogger returns a no-op logger. Used as the default.
func NoopLogger() Logger { return asterlog.Noop() }

// Str / Int / Float / Bool / Err — Field factories.
func Str(key, value string) Field   { return asterlog.Str(key, value) }
func Int(key string, v int64) Field { return asterlog.Int(key, v) }
func Float(key string, v float64) Field {
	return asterlog.Float(key, v)
}
func Bool(key string, v bool) Field { return asterlog.Bool(key, v) }
func Err(err error) Field           { return asterlog.Err(err) }
