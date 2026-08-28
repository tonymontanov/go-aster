/*
FILE: futures/account.go

DESCRIPTION:
Account domain client of the Futures profile: balances, positions, leverage,
margin type, position mode, STP mode, fills, commission rates and market
position close.

ASTER SPECIFICS:
  - GET /fapi/v3/balance             — per-asset futures wallet balances;
  - GET /fapi/v3/positionRisk        — positions (one BOTH entry per symbol in
                                       One-way mode, LONG+SHORT in Hedge mode);
  - POST /fapi/v3/leverage           — change initial leverage;
  - POST /fapi/v3/marginType         — ISOLATED/CROSSED;
  - POST/GET /fapi/v3/positionSide/dual — position mode (dualSidePosition);
  - POST/GET /fapi/v3/stpMode        — account-level self-trade prevention;
  - GET /fapi/v3/userTrades          — account fills;
  - GET /fapi/v3/commissionRate      — maker/taker rates.
ClosePosition is a client-side composite: positionRisk + reduce-only MARKET
order(s) through the TradingClient.

ERROR STRATEGY:
Local validation before network; exchange codes preserved in *aster.Error.
*/

package futures

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	aster "github.com/tonymontanov/go-aster"
	"github.com/tonymontanov/go-aster/futures/types"
	"github.com/tonymontanov/go-aster/internal/codec"
	"github.com/tonymontanov/go-aster/internal/rest"
)

// Futures account endpoint paths.
const (
	endpointBalance          = "/fapi/v3/balance"
	endpointPositionRisk     = "/fapi/v3/positionRisk"
	endpointLeverage         = "/fapi/v3/leverage"
	endpointMarginType       = "/fapi/v3/marginType"
	endpointPositionSideDual = "/fapi/v3/positionSide/dual"
	endpointStpMode          = "/fapi/v3/stpMode"
	endpointUserTrades       = "/fapi/v3/userTrades"
	endpointCommissionRate   = "/fapi/v3/commissionRate"
)

// AccountClient — account domain client.
type AccountClient struct {
	c *Client
}

// newAccountClient — internal constructor.
func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// GetBalances returns futures wallet balances per asset (GET /fapi/v3/balance).
func (a *AccountClient) GetBalances(ctx context.Context) ([]types.Balance, error) {
	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointBalance,
		Signed: true,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryQuery)},
	})
	if err != nil {
		return nil, err
	}

	var raws []rawBalance
	if err = resp.Unmarshal(&raws); err != nil {
		return nil, aster.NewError(aster.ErrorKindUnknown, 0, "account.GetBalances: parse", err)
	}

	var out []types.Balance = make([]types.Balance, 0, len(raws))
	var i int
	for i = 0; i < len(raws); i++ {
		out = append(out, convertBalance(raws[i]))
	}
	return out, nil
}

/*
GetPositions returns position entries (GET /fapi/v3/positionRisk).
symbol is optional: empty string returns positions for all symbols.
One BOTH entry per symbol in One-way mode; LONG and SHORT entries in Hedge mode.
*/
func (a *AccountClient) GetPositions(ctx context.Context, symbol string) ([]types.PositionInfo, error) {
	var params url.Values = url.Values{}
	var symbols []string
	if symbol != "" {
		params.Set("symbol", symbol)
		symbols = []string{symbol}
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointPositionRisk,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  symbols,
			Category: string(aster.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return nil, err
	}

	var raws []rawPosition
	if err = resp.Unmarshal(&raws); err != nil {
		return nil, aster.NewError(aster.ErrorKindUnknown, 0, "account.GetPositions: parse", err)
	}

	var out []types.PositionInfo = make([]types.PositionInfo, 0, len(raws))
	var i int
	for i = 0; i < len(raws); i++ {
		out = append(out, convertPosition(raws[i]))
	}
	return out, nil
}

/*
ClosePosition closes the position(s) of the symbol with reduce-only MARKET
orders. Composite operation:
 1. GetPositions(symbol);
 2. for every non-zero entry — MARKET order of the opposite side for the full
    position size (reduceOnly in One-way mode, positionSide in Hedge mode).

No-op (nil) when there is no open position. Partial failures are joined.
*/
func (a *AccountClient) ClosePosition(ctx context.Context, symbol string) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "account.ClosePosition: symbol is empty", nil)
	}

	var positions []types.PositionInfo
	var err error
	positions, err = a.GetPositions(ctx, symbol)
	if err != nil {
		return err
	}

	var closeErrs []error
	var i int
	for i = 0; i < len(positions); i++ {
		var pos types.PositionInfo = positions[i]
		if pos.PositionAmt.IsZero() {
			continue
		}

		var req types.CreateOrderRequest = types.CreateOrderRequest{
			Symbol:   symbol,
			Type:     types.OrderTypeMarket,
			Quantity: pos.PositionAmt.Abs(),
		}
		if pos.PositionAmt.IsNegative() {
			req.Side = types.SideTypeBuy
		} else {
			req.Side = types.SideTypeSell
		}
		if pos.PositionSide == types.PositionSideBoth {
			// One-way mode: reduceOnly guards against flipping the position.
			req.ReduceOnly = true
		} else {
			// Hedge mode: the position side identifies the leg to reduce;
			// reduceOnly cannot be sent in Hedge mode.
			req.PositionSide = pos.PositionSide
		}

		if _, err = a.c.trading.CreateOrder(ctx, req); err != nil {
			closeErrs = append(closeErrs, err)
		}
	}
	return errors.Join(closeErrs...)
}

// SetLeverage changes the initial leverage of the symbol (POST /fapi/v3/leverage).
func (a *AccountClient) SetLeverage(ctx context.Context, symbol string, leverage int64) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "account.SetLeverage: symbol is empty", nil)
	}
	if leverage <= 0 {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "account.SetLeverage: leverage must be positive", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)
	params.Set("leverage", strconv.FormatInt(leverage, 10))

	var err error
	_, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   endpointLeverage,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryQuery),
		},
	})
	return err
}

// SetMarginType changes the margin mode of the symbol (POST /fapi/v3/marginType).
func (a *AccountClient) SetMarginType(ctx context.Context, symbol string, marginType types.MarginType) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "account.SetMarginType: symbol is empty", nil)
	}
	if marginType != types.MarginTypeIsolated && marginType != types.MarginTypeCrossed {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "account.SetMarginType: invalid margin type", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)
	params.Set("marginType", string(marginType))

	var err error
	_, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   endpointMarginType,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryQuery),
		},
	})
	return err
}

/*
SetPositionMode switches the account position mode
(POST /fapi/v3/positionSide/dual): dualSidePosition=true — Hedge mode,
false — One-way mode. Applies to EVERY symbol of the account.
*/
func (a *AccountClient) SetPositionMode(ctx context.Context, dualSidePosition bool) error {
	var params url.Values = url.Values{}
	params.Set("dualSidePosition", strconv.FormatBool(dualSidePosition))

	var err error
	_, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   endpointPositionSideDual,
		Params: params,
		Signed: true,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryQuery)},
	})
	return err
}

// GetPositionMode returns the account position mode
// (GET /fapi/v3/positionSide/dual): true — Hedge mode, false — One-way mode.
func (a *AccountClient) GetPositionMode(ctx context.Context) (bool, error) {
	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointPositionSideDual,
		Signed: true,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryQuery)},
	})
	if err != nil {
		return false, err
	}

	var raw struct {
		DualSidePosition bool `json:"dualSidePosition"`
	}
	if err = resp.Unmarshal(&raw); err != nil {
		return false, aster.NewError(aster.ErrorKindUnknown, 0, "account.GetPositionMode: parse", err)
	}
	return raw.DualSidePosition, nil
}

// SetStpMode sets the account-level self-trade prevention mode
// (POST /fapi/v3/stpMode). Per-order override: CreateOrderRequest.StpMode.
func (a *AccountClient) SetStpMode(ctx context.Context, mode types.StpMode) error {
	if mode == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "account.SetStpMode: mode is empty", nil)
	}

	var params url.Values = url.Values{}
	params.Set("stpMode", string(mode))

	var err error
	_, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   endpointStpMode,
		Params: params,
		Signed: true,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryQuery)},
	})
	return err
}

// GetStpMode returns the account-level self-trade prevention mode
// (GET /fapi/v3/stpMode).
func (a *AccountClient) GetStpMode(ctx context.Context) (types.StpMode, error) {
	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointStpMode,
		Signed: true,
		Meta:   rest.RequestMeta{Category: string(aster.RateLimitCategoryQuery)},
	})
	if err != nil {
		return "", err
	}

	var raw struct {
		StpMode string `json:"stpMode"`
	}
	if err = resp.Unmarshal(&raw); err != nil {
		return "", aster.NewError(aster.ErrorKindUnknown, 0, "account.GetStpMode: parse", err)
	}
	return types.StpMode(raw.StpMode), nil
}

/*
GetUserTrades returns account fills of the symbol (GET /fapi/v3/userTrades).
Optional: fromTradeID (id >=), startTimeMs/endTimeMs window, limit (default
500, max 1000). Zero values are omitted.
*/
func (a *AccountClient) GetUserTrades(ctx context.Context, symbol string, fromTradeID int64, startTimeMs, endTimeMs int64, limit int) ([]types.TradeInfo, error) {
	if symbol == "" {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "account.GetUserTrades: symbol is empty", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)
	if fromTradeID != 0 {
		params.Set("fromId", strconv.FormatInt(fromTradeID, 10))
	}
	if startTimeMs != 0 {
		params.Set("startTime", strconv.FormatInt(startTimeMs, 10))
	}
	if endTimeMs != 0 {
		params.Set("endTime", strconv.FormatInt(endTimeMs, 10))
	}
	if limit != 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointUserTrades,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return nil, err
	}

	var raws []rawTrade
	if err = resp.Unmarshal(&raws); err != nil {
		return nil, aster.NewError(aster.ErrorKindUnknown, 0, "account.GetUserTrades: parse", err)
	}

	var out []types.TradeInfo = make([]types.TradeInfo, 0, len(raws))
	var i int
	for i = 0; i < len(raws); i++ {
		out = append(out, convertTrade(raws[i]))
	}
	return out, nil
}

// GetCommissionRate returns maker/taker commission rates of the symbol
// (GET /fapi/v3/commissionRate).
func (a *AccountClient) GetCommissionRate(ctx context.Context, symbol string) (types.CommissionRate, error) {
	var out types.CommissionRate
	if symbol == "" {
		return out, aster.NewError(aster.ErrorKindInvalidRequest, 0, "account.GetCommissionRate: symbol is empty", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointCommissionRate,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return out, err
	}

	var raw struct {
		Symbol              string `json:"symbol"`
		MakerCommissionRate string `json:"makerCommissionRate"`
		TakerCommissionRate string `json:"takerCommissionRate"`
	}
	if err = resp.Unmarshal(&raw); err != nil {
		return out, aster.NewError(aster.ErrorKindUnknown, 0, "account.GetCommissionRate: parse", err)
	}
	out.Symbol = raw.Symbol
	out.MakerCommissionRate, _ = codec.ParseDecimal(raw.MakerCommissionRate)
	out.TakerCommissionRate, _ = codec.ParseDecimal(raw.TakerCommissionRate)
	return out, nil
}

// rawBalance — wire balance entry.
type rawBalance struct {
	AccountAlias       string `json:"accountAlias"`
	Asset              string `json:"asset"`
	Balance            string `json:"balance"`
	CrossWalletBalance string `json:"crossWalletBalance"`
	CrossUnPnl         string `json:"crossUnPnl"`
	AvailableBalance   string `json:"availableBalance"`
	MaxWithdrawAmount  string `json:"maxWithdrawAmount"`
	MarginAvailable    bool   `json:"marginAvailable"`
	UpdateTime         int64  `json:"updateTime"`
}

// convertBalance maps a rawBalance into the domain Balance.
func convertBalance(raw rawBalance) types.Balance {
	var out types.Balance = types.Balance{
		AccountAlias:    raw.AccountAlias,
		Asset:           raw.Asset,
		MarginAvailable: raw.MarginAvailable,
		UpdateTime:      raw.UpdateTime,
	}
	out.WalletBalance, _ = codec.ParseDecimal(raw.Balance)
	out.CrossWalletBalance, _ = codec.ParseDecimal(raw.CrossWalletBalance)
	out.CrossUnPnl, _ = codec.ParseDecimal(raw.CrossUnPnl)
	out.AvailableBalance, _ = codec.ParseDecimal(raw.AvailableBalance)
	out.MaxWithdrawAmount, _ = codec.ParseDecimal(raw.MaxWithdrawAmount)
	return out
}

// rawPosition — wire positionRisk entry. Note: isAutoAddMargin arrives as a
// string ("true"/"false"), leverage as a numeric string.
type rawPosition struct {
	Symbol           string `json:"symbol"`
	PositionSide     string `json:"positionSide"`
	PositionAmt      string `json:"positionAmt"`
	EntryPrice       string `json:"entryPrice"`
	MarkPrice        string `json:"markPrice"`
	UnRealizedProfit string `json:"unRealizedProfit"`
	LiquidationPrice string `json:"liquidationPrice"`
	Leverage         string `json:"leverage"`
	MarginType       string `json:"marginType"`
	IsAutoAddMargin  string `json:"isAutoAddMargin"`
	IsolatedMargin   string `json:"isolatedMargin"`
	MaxNotionalValue string `json:"maxNotionalValue"`
	UpdateTime       int64  `json:"updateTime"`
}

// convertPosition maps a rawPosition into the domain PositionInfo.
func convertPosition(raw rawPosition) types.PositionInfo {
	var out types.PositionInfo = types.PositionInfo{
		Symbol:          raw.Symbol,
		PositionSide:    types.PositionSide(raw.PositionSide),
		MarginType:      types.ParseMarginType(raw.MarginType),
		IsAutoAddMargin: raw.IsAutoAddMargin == "true",
		UpdateTime:      raw.UpdateTime,
	}
	out.PositionAmt, _ = codec.ParseDecimal(raw.PositionAmt)
	out.EntryPrice, _ = codec.ParseDecimal(raw.EntryPrice)
	out.MarkPrice, _ = codec.ParseDecimal(raw.MarkPrice)
	out.UnrealizedProfit, _ = codec.ParseDecimal(raw.UnRealizedProfit)
	out.LiquidationPrice, _ = codec.ParseDecimal(raw.LiquidationPrice)
	out.IsolatedMargin, _ = codec.ParseDecimal(raw.IsolatedMargin)
	out.MaxNotionalValue, _ = codec.ParseDecimal(raw.MaxNotionalValue)
	out.Leverage, _ = codec.ParseInt64(raw.Leverage)
	return out
}

// rawTrade — wire userTrades entry.
type rawTrade struct {
	Symbol          string `json:"symbol"`
	ID              int64  `json:"id"`
	OrderID         int64  `json:"orderId"`
	Side            string `json:"side"`
	PositionSide    string `json:"positionSide"`
	Price           string `json:"price"`
	Qty             string `json:"qty"`
	QuoteQty        string `json:"quoteQty"`
	RealizedPnl     string `json:"realizedPnl"`
	Commission      string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
	Time            int64  `json:"time"`
	Maker           bool   `json:"maker"`
	Buyer           bool   `json:"buyer"`
}

// convertTrade maps a rawTrade into the domain TradeInfo.
func convertTrade(raw rawTrade) types.TradeInfo {
	var out types.TradeInfo = types.TradeInfo{
		Symbol:          raw.Symbol,
		ID:              raw.ID,
		OrderID:         raw.OrderID,
		Side:            types.SideType(raw.Side),
		PositionSide:    types.PositionSide(raw.PositionSide),
		CommissionAsset: raw.CommissionAsset,
		Time:            raw.Time,
		Maker:           raw.Maker,
		Buyer:           raw.Buyer,
	}
	out.Price, _ = codec.ParseDecimal(raw.Price)
	out.Quantity, _ = codec.ParseDecimal(raw.Qty)
	out.QuoteQuantity, _ = codec.ParseDecimal(raw.QuoteQty)
	out.RealizedPnl, _ = codec.ParseDecimal(raw.RealizedPnl)
	out.Commission, _ = codec.ParseDecimal(raw.Commission)
	return out
}
