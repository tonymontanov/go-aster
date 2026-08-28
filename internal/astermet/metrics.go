/*
FILE: internal/astermet/metrics.go

DESCRIPTION:
Minimal counter and counter-factory interface for the SDK. Shaped like
prometheus.Counter (Inc/Add) but not tied to Prometheus — the user attaches
any backend (Prometheus, OpenTelemetry, statsd) via a thin 5–10 line adapter.

MAIN ENTITIES:
  - Counter:        counter interface. Inc() and Add(float64).
  - CounterFactory: factory returning a Counter by name + label pairs
                    (k1, v1, k2, v2 ...). Label pairs serve two purposes:
                      1. compatibility with prometheus.NewCounterVec.WithLabelValues;
                      2. no map[string]string allocation per call in hot paths.
  - Noop:           default implementation, all calls are no-ops.

COUNTER NAMING IN THE SDK:
The SDK decides which counters to create and under what names. Names are
chosen to be stable and predictable so that user bench/grafana dashboards
work without rewriting:
  aster_ws_messages_received_total
  aster_ws_messages_dropped_total
  aster_ws_reconnects_total
  aster_ws_subscriptions_total
  aster_ws_listen_key_renewals_total
*/

package astermet

// Counter — a single counter (monotonically increasing number).
type Counter interface {
	// Inc increments the value by 1.
	Inc()
	// Add increments the value by delta. If delta < 0, the implementation may
	// ignore it (counter is monotonic).
	Add(delta float64)
}

// CounterFactory — counter factory. labels — k1, v1, k2, v2, ... .
type CounterFactory interface {
	// Counter returns a counter by name and labels. The implementation must
	// guarantee that the same (name, labels) set always returns the same
	// entry (e.g. via sync.Map).
	Counter(name string, labels ...string) Counter
}

// Noop — default implementation.
type noopFactory struct{}
type noopCounter struct{}

// Noop returns a no-op factory (singleton).
func Noop() CounterFactory { return noopFactorySingleton }

var (
	noopFactorySingleton CounterFactory = noopFactory{}
	noopCounterSingleton Counter        = noopCounter{}
)

func (noopFactory) Counter(string, ...string) Counter { return noopCounterSingleton }

func (noopCounter) Inc()        {}
func (noopCounter) Add(float64) {}
