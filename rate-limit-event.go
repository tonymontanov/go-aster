/*
FILE: rate-limit-event.go

DESCRIPTION:
Public RateLimitEvent type that the SDK delivers to subscribers via
aster.Config.RateLimitEventObserver.

ASTER SPECIFICS:
Unlike OKX, Aster (Binance-style) DOES return rate-limit usage counters in
response headers:

	X-MBX-USED-WEIGHT-(intervalNum)(intervalLetter)  — request-weight budget per IP;
	X-MBX-ORDER-COUNT-(intervalNum)(intervalLetter)  — order-count budget per account.

The SDK collects these headers on every REST response and forwards them to the
observer, together with the request metadata (OrderCount / Symbols / Category)
that an external rate-limiter needs to model per-endpoint weights and batch
accounting. Budget limits themselves come from GET /fapi/v3/exchangeInfo
(rateLimits array: REQUEST_WEIGHT, ORDERS).

DEPENDENCIES:
None — this is a plain data struct.
*/

package aster

// RateLimitCategory — REST call classification from the Aster rate-limit model
// perspective. Used by external rate-limiters to distribute usage across
// different limit planes (request weight per IP, order count per account).
type RateLimitCategory string

const (
	// RateLimitCategoryPlace — order creation.
	// Endpoints: POST /fapi/v3/order, POST /fapi/v3/batchOrders.
	// Counted against the ORDERS budget in addition to REQUEST_WEIGHT.
	RateLimitCategoryPlace RateLimitCategory = "place"

	// RateLimitCategoryAmend — order modification.
	// Endpoints: PUT /fapi/v3/order, PUT /fapi/v3/batchOrders.
	RateLimitCategoryAmend RateLimitCategory = "amend"

	// RateLimitCategoryCancel — order cancellation.
	// Endpoints: DELETE /fapi/v3/order, DELETE /fapi/v3/batchOrders,
	// DELETE /fapi/v3/allOpenOrders.
	RateLimitCategoryCancel RateLimitCategory = "cancel"

	// RateLimitCategoryQuery — private GET or non-trading POST (leverage,
	// margin type, position mode, listenKey).
	RateLimitCategoryQuery RateLimitCategory = "query"

	// RateLimitCategoryMarketData — public GET (per-IP request weight only).
	// Endpoints: /fapi/v3/depth, /fapi/v3/klines, /fapi/v3/exchangeInfo, ...
	RateLimitCategoryMarketData RateLimitCategory = "market"

	// RateLimitCategoryUnknown — fallback for requests not covered by any
	// explicit category. The external rate-limiter may either ignore such
	// events or count them conservatively as Query.
	RateLimitCategoryUnknown RateLimitCategory = ""
)

// String returns the string representation of the category.
func (c RateLimitCategory) String() string {
	return string(c)
}

// RateLimitEvent — structured rate-limit event that the SDK delivers to
// subscribers via aster.Config.RateLimitEventObserver.
//
// All fields are populated by the SDK strictly after an HTTP response is
// received (even if the response carries an exchange-level error): the
// observer is called exactly once per completed REST call. It is NOT called
// on transport failures (timeout, connection reset) because no new rate-limit
// information exists in those cases.
type RateLimitEvent struct {
	// Endpoint — request path (e.g. "/fapi/v3/batchOrders"). Never empty.
	Endpoint string

	// Method — HTTP request method in upper case (GET / POST / PUT / DELETE).
	Method string

	// Headers — rate-limit response headers as returned by Aster, filtered to
	// the X-MBX-USED-WEIGHT-* / X-MBX-ORDER-COUNT-* families. Keys are in
	// canonical http.Header form (e.g. "X-Mbx-Used-Weight-1m"). Always non-nil;
	// may be empty when the endpoint returns no counters.
	Headers map[string]string

	// OrderCount — number of orders CREATED/AMENDED/CANCELLED by this request:
	//   - 1 for /fapi/v3/order (POST/PUT/DELETE);
	//   - len(orders) for /fapi/v3/batchOrders;
	//   - 0 for non-trading requests (account, market, public).
	OrderCount int

	// Symbols — sorted list of unique symbols the request relates to:
	//   - 1 element for single trading methods;
	//   - 1+ for batch (unique symbol set of all orders in the batch);
	//   - empty ([]string{}, not nil) for requests without a symbol.
	Symbols []string

	// Category — classification by the Aster rate-limit model. Used by the
	// external rate-limiter for the ORDERS plane (place/amend count against
	// it, queries do not) and for always-allow-Cancel policies.
	Category RateLimitCategory
}
