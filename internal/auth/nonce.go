/*
FILE: internal/auth/nonce.go

DESCRIPTION:
Monotonic microsecond nonce generator for Aster V3 requests.

ASTER SPECIFICS:
The V3 nonce is the current timestamp in MICROSECONDS (not milliseconds).
The server accepts nonces within ±60s of server time, rejects duplicates,
and keeps only the most recent 100 nonces per agent address — a nonce
smaller than the current minimum of that window is rejected as expired.
Therefore nonces must be strictly increasing even when several goroutines
request them within the same microsecond.

MAIN ENTITIES:
  - NonceGenerator: lock-free strictly-increasing microsecond timestamps.

CONCURRENCY:
Safe for concurrent use. CAS loop: if the wall clock did not advance past the
last issued nonce, last+1 is issued instead. The clock re-synchronizes
naturally as soon as real time overtakes the counter.
*/

package auth

import (
	"sync/atomic"
	"time"
)

// NonceGenerator issues strictly increasing microsecond nonces.
// The zero value is ready to use.
type NonceGenerator struct {
	last atomic.Int64
}

// Next returns the next nonce: max(now_microseconds, previous+1).
func (g *NonceGenerator) Next() int64 {
	for {
		var now int64 = time.Now().UnixMicro()
		var last int64 = g.last.Load()
		if now <= last {
			now = last + 1
		}
		if g.last.CompareAndSwap(last, now) {
			return now
		}
	}
}
