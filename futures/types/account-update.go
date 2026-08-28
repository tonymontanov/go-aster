/*
FILE: futures/types/account-update.go

DESCRIPTION:
Balance and position update from the user data stream (ACCOUNT_UPDATE event).
Pushed on any account change (fills, funding, margin transfers, deposits).
Only balances/positions that actually changed are included.
*/

package types

import "github.com/shopspring/decimal"

// AccountUpdateReason — event reason (a.m field). Values per the docs:
// DEPOSIT, WITHDRAW, ORDER, FUNDING_FEE, WITHDRAW_REJECT, ADJUSTMENT,
// INSURANCE_CLEAR, ADMIN_DEPOSIT, ADMIN_WITHDRAW, MARGIN_TRANSFER,
// MARGIN_TYPE_CHANGE, ASSET_TRANSFER, OPTIONS_PREMIUM_FEE,
// OPTIONS_SETTLE_PROFIT, AUTO_EXCHANGE.
type AccountUpdateReason string

const (
	AccountUpdateReasonOrder      AccountUpdateReason = "ORDER"
	AccountUpdateReasonFundingFee AccountUpdateReason = "FUNDING_FEE"
)

// BalanceUpdate — one changed balance (a.B[] entry).
type BalanceUpdate struct {
	// Asset — asset name.
	Asset string
	// WalletBalance — wallet balance after the change.
	WalletBalance decimal.Decimal
	// CrossWalletBalance — cross wallet balance after the change.
	CrossWalletBalance decimal.Decimal
	// BalanceChange — balance change EXCEPT PnL and commission.
	BalanceChange decimal.Decimal
}

// PositionUpdate — one changed position (a.P[] entry).
type PositionUpdate struct {
	// Symbol — trading pair.
	Symbol string
	// PositionSide — BOTH/LONG/SHORT.
	PositionSide PositionSide
	// PositionAmt — signed position size after the change.
	PositionAmt decimal.Decimal
	// EntryPrice — average entry price.
	EntryPrice decimal.Decimal
	// AccumulatedRealized — pre-fee accumulated realized PnL (cr field).
	AccumulatedRealized decimal.Decimal
	// UnrealizedPnl — unrealized PnL.
	UnrealizedPnl decimal.Decimal
	// MarginType — ISOLATED/CROSSED.
	MarginType MarginType
	// IsolatedWallet — isolated wallet amount (isolated positions).
	IsolatedWallet decimal.Decimal
}

// AccountUpdate — one ACCOUNT_UPDATE event.
type AccountUpdate struct {
	// EventTs / TxTs — event and transaction times, ms.
	EventTs int64
	TxTs    int64
	// Reason — what triggered the update.
	Reason AccountUpdateReason
	// Balances — changed balances only.
	Balances []BalanceUpdate
	// Positions — changed positions only (non-zero amount or isolated wallet).
	Positions []PositionUpdate
}
