/*
FILE: metrics.go

DESCRIPTION:
Public re-export of the metrics interface. The interface itself lives in
internal/astermet so that any internal SDK package (ws, rest) can use it
without an import cycle. The root package re-exports the type/functions
via alias.

PROMETHEUS INTEGRATION:
A typical adapter in user code looks like this:

	type promFactory struct{ namespace string }
	func (p promFactory) Counter(name string, labels ...string) aster.Counter {
		var c prometheus.Counter = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: p.namespace, Name: name,
		})
		// labels can be forwarded to CounterVec.With(...).
		return c
	}

	cfg.Metrics = promFactory{namespace: "myapp"}

Since prometheus.Counter already has Inc/Add methods it implements
aster.Counter without any wrapper.
*/

package aster

import "github.com/tonymontanov/go-aster/internal/astermet"

// Counter — metrics counter. Alias.
type Counter = astermet.Counter

// CounterFactory — counter factory. Alias.
type CounterFactory = astermet.CounterFactory

// NoopMetrics returns a no-op counter factory. Used as the default.
func NoopMetrics() CounterFactory { return astermet.Noop() }
