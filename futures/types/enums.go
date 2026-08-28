/*
FILE: futures/types/enums.go

DESCRIPTION:
Futures-profile-specific enums (absent from spot): position side, trigger
working type, margin type, contract type/status. Values exactly match the
Aster V3 wire protocol.
*/

package types

// PositionSide — position side. BOTH in One-way mode; LONG/SHORT in Hedge mode.
type PositionSide string

const (
	// PositionSideBoth — One-way mode (default).
	PositionSideBoth PositionSide = "BOTH"
	// PositionSideLong — long leg in Hedge mode.
	PositionSideLong PositionSide = "LONG"
	// PositionSideShort — short leg in Hedge mode.
	PositionSideShort PositionSide = "SHORT"
)

// WorkingType — which price triggers conditional orders (stopPrice).
type WorkingType string

const (
	// WorkingTypeMarkPrice — trigger by mark price.
	WorkingTypeMarkPrice WorkingType = "MARK_PRICE"
	// WorkingTypeContractPrice — trigger by last (contract) price. Default.
	WorkingTypeContractPrice WorkingType = "CONTRACT_PRICE"
)

// MarginType — margin mode of a symbol position. Note the wire asymmetry
// inherited from Binance: the change endpoint accepts ISOLATED/CROSSED
// (upper case), while positionRisk returns "isolated"/"cross" (lower case) —
// use ParseMarginType for responses.
type MarginType string

const (
	// MarginTypeIsolated — isolated margin.
	MarginTypeIsolated MarginType = "ISOLATED"
	// MarginTypeCrossed — cross margin.
	MarginTypeCrossed MarginType = "CROSSED"
)

// ParseMarginType parses a margin type from any wire representation
// ("isolated"/"cross"/"crossed", any case) into the canonical enum.
func ParseMarginType(s string) MarginType {
	switch s {
	case "isolated", "ISOLATED":
		return MarginTypeIsolated
	default:
		return MarginTypeCrossed
	}
}

// ContractType — futures contract type. Aster v1 lists only PERPETUAL.
type ContractType string

const (
	// ContractTypePerpetual — perpetual futures contract.
	ContractTypePerpetual ContractType = "PERPETUAL"
)

// ContractStatus — symbol trading status (contractStatus / status field).
type ContractStatus string

const (
	ContractStatusPendingTrading ContractStatus = "PENDING_TRADING"
	ContractStatusTrading        ContractStatus = "TRADING"
	ContractStatusPreSettle      ContractStatus = "PRE_SETTLE"
	ContractStatusSettling       ContractStatus = "SETTLING"
	ContractStatusClose          ContractStatus = "CLOSE"
)
