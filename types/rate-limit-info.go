/*
FILE: types/rate-limit-info.go

DESCRIPTION:
Exchange rate-limit descriptor from the exchangeInfo endpoint (rateLimits
array). Used to initialize external rate-limiter budgets.
*/

package types

// RateLimitType — budget family.
type RateLimitType string

const (
	// RateLimitTypeRequestWeight — per-IP request weight budget.
	RateLimitTypeRequestWeight RateLimitType = "REQUEST_WEIGHT"
	// RateLimitTypeOrders — per-account order count budget.
	RateLimitTypeOrders RateLimitType = "ORDERS"
)

// RateLimitInfo — one rateLimits entry from exchangeInfo.
type RateLimitInfo struct {
	// RateLimitType — REQUEST_WEIGHT / ORDERS.
	RateLimitType RateLimitType
	// Interval — window unit (e.g. "MINUTE", "SECOND").
	Interval string
	// IntervalNum — number of interval units in the window.
	IntervalNum int
	// Limit — budget for the window.
	Limit int64
}
