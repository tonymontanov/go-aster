/*
FILE: futures/types/balance.go

DESCRIPTION:
Futures account balance entry from GET /fapi/v3/balance (one entry per asset).
*/

package types

import "github.com/shopspring/decimal"

// Balance — futures wallet balance for one asset.
type Balance struct {
	// AccountAlias — unique account code.
	AccountAlias string
	// Asset — asset name (e.g. "USDT").
	Asset string
	// WalletBalance — total wallet balance.
	WalletBalance decimal.Decimal
	// CrossWalletBalance — crossed wallet balance.
	CrossWalletBalance decimal.Decimal
	// CrossUnPnl — unrealized profit of crossed positions.
	CrossUnPnl decimal.Decimal
	// AvailableBalance — available balance.
	AvailableBalance decimal.Decimal
	// MaxWithdrawAmount — maximum amount for transfer out.
	MaxWithdrawAmount decimal.Decimal
	// MarginAvailable — usable as margin in Multi-Assets mode.
	MarginAvailable bool
	// UpdateTime — last update time, ms.
	UpdateTime int64
}
