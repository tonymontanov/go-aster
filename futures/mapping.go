/*
FILE: futures/mapping.go

DESCRIPTION:
Wire ↔ domain mapping for the futures trading endpoints:
  - buildCreateOrderFields / buildModifyOrderFields — request validation and
    wire field assembly (shared by single and batch methods so that error
    messages and payloads stay identical);
  - rawOrder / convertOrder — order response payload decoding;
  - parseBatchOrderRows — per-row union parsing of batch responses
    (an OrderInfo payload or {code,msg} per row);
  - small helpers (fieldsToValues, uniqueSorted).

CONVENTIONS:
Numeric wire fields arrive as strings and are converted via codec.ParseDecimal
with the error ignored for optional fields (zero value on malformed input) —
a single malformed optional field must not discard the entire order payload.
*/

package futures

import (
	"net/url"
	"sort"
	"strconv"

	aster "github.com/tonymontanov/go-aster"
	"github.com/tonymontanov/go-aster/futures/types"
	"github.com/tonymontanov/go-aster/internal/codec"
	"github.com/tonymontanov/go-aster/internal/rest"
)

// buildCreateOrderFields validates a CreateOrderRequest and assembles the
// wire fields. Used by CreateOrder (converted to url.Values) and
// CreateBatchOrders (marshalled into the batchOrders JSON list).
func buildCreateOrderFields(req types.CreateOrderRequest) (map[string]string, error) {
	if req.Symbol == "" {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: Symbol is empty", nil)
	}
	if req.Side == "" {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: Side is empty", nil)
	}
	if req.Type == "" {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: Type is empty", nil)
	}

	// Type-specific mandatory parameters (per the Aster New Order table).
	switch req.Type {
	case types.OrderTypeLimit:
		if req.TimeInForce == "" {
			return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: TimeInForce is required for LIMIT", nil)
		}
		if req.Quantity.IsZero() || req.Price.IsZero() {
			return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: Quantity and Price are required for LIMIT", nil)
		}
	case types.OrderTypeMarket:
		if req.Quantity.IsZero() {
			return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: Quantity is required for MARKET", nil)
		}
	case types.OrderTypeStop, types.OrderTypeTakeProfit:
		if req.Quantity.IsZero() || req.Price.IsZero() || req.StopPrice.IsZero() {
			return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: Quantity, Price and StopPrice are required for "+string(req.Type), nil)
		}
	case types.OrderTypeStopMarket, types.OrderTypeTakeProfitMarket:
		if req.StopPrice.IsZero() && !req.ClosePosition {
			return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: StopPrice is required for "+string(req.Type), nil)
		}
	case types.OrderTypeTrailingStopMarket:
		if req.CallbackRate.IsZero() {
			return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: CallbackRate is required for TRAILING_STOP_MARKET", nil)
		}
	}

	if req.ClosePosition && (!req.Quantity.IsZero() || req.ReduceOnly) {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: ClosePosition cannot be combined with Quantity or ReduceOnly", nil)
	}

	var fields map[string]string = map[string]string{
		"symbol": req.Symbol,
		"side":   string(req.Side),
		"type":   string(req.Type),
	}
	if req.PositionSide != "" {
		fields["positionSide"] = string(req.PositionSide)
	}
	if req.TimeInForce != "" {
		fields["timeInForce"] = string(req.TimeInForce)
	}
	if !req.Quantity.IsZero() {
		fields["quantity"] = req.Quantity.String()
	}
	if !req.Price.IsZero() {
		fields["price"] = req.Price.String()
	}
	if req.ClientOrderID != "" {
		fields["newClientOrderId"] = req.ClientOrderID
	}
	if req.ReduceOnly {
		fields["reduceOnly"] = "true"
	}
	if req.ClosePosition {
		fields["closePosition"] = "true"
	}
	if !req.StopPrice.IsZero() {
		fields["stopPrice"] = req.StopPrice.String()
	}
	if !req.ActivationPrice.IsZero() {
		fields["activationPrice"] = req.ActivationPrice.String()
	}
	if !req.CallbackRate.IsZero() {
		fields["callbackRate"] = req.CallbackRate.String()
	}
	if req.WorkingType != "" {
		fields["workingType"] = string(req.WorkingType)
	}
	if req.PriceProtect {
		fields["priceProtect"] = "TRUE"
	}
	if req.NewOrderRespType != "" {
		fields["newOrderRespType"] = string(req.NewOrderRespType)
	}
	if req.StpMode != "" {
		fields["stpMode"] = string(req.StpMode)
	}
	return fields, nil
}

// buildModifyOrderFields validates a ModifyOrderRequest and assembles the
// wire fields. Shared by ModifyOrder and ModifyBatchOrders.
func buildModifyOrderFields(req types.ModifyOrderRequest) (map[string]string, error) {
	if req.Symbol == "" {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: Symbol is empty", nil)
	}
	if req.OrderID == 0 && req.OrigClientOrderID == "" {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: OrderID or OrigClientOrderID is required", nil)
	}
	if req.Quantity.IsZero() || req.Price.IsZero() {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading: Quantity and Price are both required for modify", nil)
	}

	var fields map[string]string = map[string]string{
		"symbol":   req.Symbol,
		"quantity": req.Quantity.String(),
		"price":    req.Price.String(),
	}
	if req.OrderID != 0 {
		fields["orderId"] = formatInt(req.OrderID)
	} else {
		fields["origClientOrderId"] = req.OrigClientOrderID
	}
	return fields, nil
}

// fieldsToValues converts a wire field map into url.Values.
func fieldsToValues(fields map[string]string) url.Values {
	var params url.Values = url.Values{}
	var k, v string
	for k, v = range fields {
		params.Set(k, v)
	}
	return params
}

// uniqueSorted returns the sorted set of unique non-empty strings.
func uniqueSorted(in []string) []string {
	var set map[string]struct{} = map[string]struct{}{}
	var i int
	for i = 0; i < len(in); i++ {
		if in[i] != "" {
			set[in[i]] = struct{}{}
		}
	}
	var out []string = make([]string, 0, len(set))
	var s string
	for s = range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// formatInt — strconv.FormatInt(v, 10) shorthand used by field builders.
func formatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}

// rawOrder — order payload as delivered by the exchange (create/modify/cancel
// acks and query responses). Numeric values are strings on the wire except
// ids, times and booleans.
type rawOrder struct {
	Symbol        string `json:"symbol"`
	OrderID       int64  `json:"orderId"`
	ClientOrderID string `json:"clientOrderId"`
	Side          string `json:"side"`
	PositionSide  string `json:"positionSide"`
	Status        string `json:"status"`
	Price         string `json:"price"`
	AvgPrice      string `json:"avgPrice"`
	OrigQty       string `json:"origQty"`
	ExecutedQty   string `json:"executedQty"`
	CumQuote      string `json:"cumQuote"`
	StopPrice     string `json:"stopPrice"`
	ReduceOnly    bool   `json:"reduceOnly"`
	ClosePosition bool   `json:"closePosition"`
	TimeInForce   string `json:"timeInForce"`
	Type          string `json:"type"`
	OrigType      string `json:"origType"`
	ActivatePrice string `json:"activatePrice"`
	PriceRate     string `json:"priceRate"`
	WorkingType   string `json:"workingType"`
	PriceProtect  bool   `json:"priceProtect"`
	Time          int64  `json:"time"`
	UpdateTime    int64  `json:"updateTime"`
}

// convertOrder maps a rawOrder into the domain OrderInfo.
func convertOrder(raw rawOrder, rateLimits map[string]string) types.OrderInfo {
	var info types.OrderInfo = types.OrderInfo{
		Symbol:        raw.Symbol,
		OrderID:       raw.OrderID,
		ClientOrderID: raw.ClientOrderID,
		Side:          types.SideType(raw.Side),
		PositionSide:  types.PositionSide(raw.PositionSide),
		Type:          types.OrderType(raw.Type),
		OrigType:      types.OrderType(raw.OrigType),
		TimeInForce:   types.TimeInForceType(raw.TimeInForce),
		Status:        types.ParseOrderStatus(raw.Status),
		ReduceOnly:    raw.ReduceOnly,
		ClosePosition: raw.ClosePosition,
		WorkingType:   types.WorkingType(raw.WorkingType),
		PriceProtect:  raw.PriceProtect,
		CreatedTime:   raw.Time,
		UpdateTime:    raw.UpdateTime,
		RateLimits:    rateLimits,
	}
	info.Price, _ = codec.ParseDecimal(raw.Price)
	info.AvgPrice, _ = codec.ParseDecimal(raw.AvgPrice)
	info.OrigQuantity, _ = codec.ParseDecimal(raw.OrigQty)
	info.ExecutedQuantity, _ = codec.ParseDecimal(raw.ExecutedQty)
	info.CumQuote, _ = codec.ParseDecimal(raw.CumQuote)
	info.StopPrice, _ = codec.ParseDecimal(raw.StopPrice)
	info.ActivatePrice, _ = codec.ParseDecimal(raw.ActivatePrice)
	info.PriceRate, _ = codec.ParseDecimal(raw.PriceRate)
	// Placement/cancel acks carry no creation time — anchor on updateTime.
	if info.CreatedTime == 0 {
		info.CreatedTime = raw.UpdateTime
	}
	return info
}

// batchRowProbe — discriminator for batch response rows: an error row is
// {"code": <negative>, "msg": "..."}; an order row has no code field.
type batchRowProbe struct {
	Code int64  `json:"code"`
	Msg  string `json:"msg"`
}

/*
parseBatchOrderRows decodes a batch response ([]row where each row is an
OrderInfo payload or {code,msg}). Returns accepted orders, per-row errors,
and a fatal parse error (nil in the normal path). Accepted orders are
registered in the id-mapping cache.
*/
func (t *TradingClient) parseBatchOrderRows(resp rest.Response, rateLimits map[string]string, opName string) ([]types.OrderInfo, []error, error) {
	var rows []codec.RawMessage
	var err error
	if err = resp.Unmarshal(&rows); err != nil {
		return nil, nil, aster.NewError(aster.ErrorKindUnknown, 0, opName+": parse batch response", err)
	}

	var infos []types.OrderInfo = make([]types.OrderInfo, 0, len(rows))
	var rowErrs []error
	var i int
	for i = 0; i < len(rows); i++ {
		var probe batchRowProbe
		if err = codec.Unmarshal(rows[i], &probe); err == nil && probe.Code < 0 {
			rowErrs = append(rowErrs, &aster.Error{
				Kind:      aster.MapAsterCode(probe.Code, probe.Msg),
				AsterCode: probe.Code,
				Message:   probe.Msg,
			})
			continue
		}
		var raw rawOrder
		if err = codec.Unmarshal(rows[i], &raw); err != nil {
			rowErrs = append(rowErrs, aster.NewError(aster.ErrorKindUnknown, 0, opName+": parse batch row", err))
			continue
		}
		var info types.OrderInfo = convertOrder(raw, rateLimits)
		t.rememberMapping(info.ClientOrderID, info.OrderID, info.UpdateTime)
		infos = append(infos, info)
	}
	return infos, rowErrs, nil
}
