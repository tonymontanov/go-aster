/*
FILE: futures/types/exchange-info.go

DESCRIPTION:
Exchange-wide trading rules from GET /fapi/v3/exchangeInfo: rate-limit
budgets, margin assets and per-symbol trading rules.
*/

package types

// AssetInfo — one assets[] entry of exchangeInfo.
type AssetInfo struct {
	// Asset — asset name.
	Asset string
	// MarginAvailable — usable as margin in Multi-Assets mode.
	MarginAvailable bool
}

// ExchangeInfo — exchange-wide trading rules.
type ExchangeInfo struct {
	// Timezone — exchange timezone ("UTC").
	Timezone string
	// ServerTime — server time, ms (per docs prefer GET /fapi/v3/time).
	ServerTime int64
	// RateLimits — REQUEST_WEIGHT / ORDERS budgets.
	RateLimits []RateLimitInfo
	// Assets — margin assets.
	Assets []AssetInfo
	// Symbols — per-symbol trading rules.
	Symbols []SymbolInfo
}
