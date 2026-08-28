/*
FILE: futures/trading.go

DESCRIPTION:
Trading domain client of the Futures profile: order create/modify/cancel,
batch variants, cancel-all, cancel-forgotten (TTL), open/all order queries and
ClientOrderID ↔ OrderID mapping.

ASTER SPECIFICS:
  - POST /fapi/v3/order            — create (weight 1);
  - PUT /fapi/v3/order             — modify (LIMIT only, price+quantity both required);
  - DELETE /fapi/v3/order          — cancel by orderId or origClientOrderId;
  - POST/PUT /fapi/v3/batchOrders  — batch create/modify, max 5 per request
                                     (10 for MM-whitelisted accounts — the SDK
                                     chunks at 5, the safe bound);
  - DELETE /fapi/v3/batchOrders    — batch cancel, max 10 ids per request;
  - DELETE /fapi/v3/allOpenOrders  — cancel everything for a symbol;
  - POST /fapi/v3/countdownCancelAll — dead-man-switch cancel timer.
Batch responses are per-row unions: an OrderInfo payload or {code,msg}. The
SDK returns successfully accepted rows and joins row failures via errors.Join.

ERROR STRATEGY:
Local validation errors are returned BEFORE any network call with
ErrorKindInvalidRequest. Exchange rejections keep their code in *aster.Error.

CONCURRENCY:
Safe for concurrent use. The id-mapping cache is guarded by an RWMutex.
*/

package futures

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	aster "github.com/tonymontanov/go-aster"
	"github.com/tonymontanov/go-aster/futures/types"
	"github.com/tonymontanov/go-aster/internal/codec"
	"github.com/tonymontanov/go-aster/internal/rest"
)

// Futures trading endpoint paths.
const (
	endpointOrder           = "/fapi/v3/order"
	endpointBatchOrders     = "/fapi/v3/batchOrders"
	endpointAllOpenOrders   = "/fapi/v3/allOpenOrders"
	endpointCountdownCancel = "/fapi/v3/countdownCancelAll"
	endpointOpenOrder       = "/fapi/v3/openOrder"
	endpointOpenOrders      = "/fapi/v3/openOrders"
	endpointAllOrders       = "/fapi/v3/allOrders"
)

// Batch size limits fixed by the exchange protocol.
const (
	// MaxBatchSize — max orders per batch create/modify request (the safe
	// bound; MM-whitelisted accounts may send 10, but the SDK chunks at 5).
	MaxBatchSize = 5
	// MaxCancelBatchSize — max ids per batch cancel request.
	MaxCancelBatchSize = 10
)

// TradingClient — trading domain client.
type TradingClient struct {
	c *Client

	// ClientOrderID ↔ OrderID mapping cache. Maintained from order
	// acknowledgements; refreshed via SyncOrderMappings.
	mu          sync.RWMutex
	clOrdToOrd  map[string]int64
	ordToClOrd  map[int64]string
	createdAtMs map[string]int64
}

// newTradingClient — internal constructor.
func newTradingClient(c *Client) *TradingClient {
	return &TradingClient{
		c:           c,
		clOrdToOrd:  map[string]int64{},
		ordToClOrd:  map[int64]string{},
		createdAtMs: map[string]int64{},
	}
}

/*
CreateOrder places a single order.

Parameters:
  - ctx: cancellation/deadline context.
  - req: order parameters (see types.CreateOrderRequest).

Returns the acknowledged OrderInfo (detail level per req.NewOrderRespType)
or an error. Validation errors are local and produce no network call.
*/
func (t *TradingClient) CreateOrder(ctx context.Context, req types.CreateOrderRequest) (types.OrderInfo, error) {
	var info types.OrderInfo
	var fields map[string]string
	var err error
	fields, err = buildCreateOrderFields(req)
	if err != nil {
		return info, err
	}

	var resp rest.Response
	var rateLimits map[string]string
	resp, rateLimits, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   endpointOrder,
		Params: fieldsToValues(fields),
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: 1,
			Symbols:    []string{req.Symbol},
			Category:   string(aster.RateLimitCategoryPlace),
		},
	})
	if err != nil {
		return info, err
	}

	var raw rawOrder
	if err = resp.Unmarshal(&raw); err != nil {
		return info, aster.NewError(aster.ErrorKindUnknown, 0, "trading.CreateOrder: parse", err)
	}
	info = convertOrder(raw, rateLimits)
	t.rememberMapping(info.ClientOrderID, info.OrderID, info.UpdateTime)
	return info, nil
}

/*
ModifyOrder modifies price/quantity of an active LIMIT order.

Aster rules: both Quantity and Price are mandatory; the order is addressed by
OrderID or OrigClientOrderID (OrderID wins). A modification violating symbol
filters is rejected and the original order remains.
*/
func (t *TradingClient) ModifyOrder(ctx context.Context, req types.ModifyOrderRequest) (types.OrderInfo, error) {
	var info types.OrderInfo
	var fields map[string]string
	var err error
	fields, err = buildModifyOrderFields(req)
	if err != nil {
		return info, err
	}

	var resp rest.Response
	var rateLimits map[string]string
	resp, rateLimits, err = t.c.rest().Do(ctx, rest.Options{
		Method: "PUT",
		Path:   endpointOrder,
		Params: fieldsToValues(fields),
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: 1,
			Symbols:    []string{req.Symbol},
			Category:   string(aster.RateLimitCategoryAmend),
		},
	})
	if err != nil {
		return info, err
	}

	var raw rawOrder
	if err = resp.Unmarshal(&raw); err != nil {
		return info, aster.NewError(aster.ErrorKindUnknown, 0, "trading.ModifyOrder: parse", err)
	}
	info = convertOrder(raw, rateLimits)
	t.rememberMapping(info.ClientOrderID, info.OrderID, info.UpdateTime)
	return info, nil
}

/*
CancelOrder cancels a single active order addressed by OrderID or
OrigClientOrderID (OrderID takes precedence).
*/
func (t *TradingClient) CancelOrder(ctx context.Context, req types.CancelOrderRequest) error {
	if req.Symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.CancelOrder: Symbol is empty", nil)
	}
	if req.OrderID == 0 && req.OrigClientOrderID == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.CancelOrder: OrderID or OrigClientOrderID is required", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", req.Symbol)
	if req.OrderID != 0 {
		params.Set("orderId", strconv.FormatInt(req.OrderID, 10))
	} else {
		params.Set("origClientOrderId", req.OrigClientOrderID)
	}

	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "DELETE",
		Path:   endpointOrder,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: 1,
			Symbols:    []string{req.Symbol},
			Category:   string(aster.RateLimitCategoryCancel),
		},
	})
	if err != nil {
		return err
	}

	var raw rawOrder
	if err = resp.Unmarshal(&raw); err == nil {
		t.forgetMapping(raw.ClientOrderID, raw.OrderID)
	}
	return nil
}

/*
CreateBatchOrders places up to N orders. Requests are chunked at MaxBatchSize
per HTTP call. Partial success semantics: accepted orders are returned; every
rejected row contributes an *aster.Error joined into the returned error
(errors.Join). len(result) == number of accepted rows.
*/
func (t *TradingClient) CreateBatchOrders(ctx context.Context, reqs []types.CreateOrderRequest) ([]types.OrderInfo, error) {
	if len(reqs) == 0 {
		return nil, nil
	}

	var accepted []types.OrderInfo = make([]types.OrderInfo, 0, len(reqs))
	var rowErrs []error

	var start int
	for start = 0; start < len(reqs); start += MaxBatchSize {
		var end int = start + MaxBatchSize
		if end > len(reqs) {
			end = len(reqs)
		}
		var chunk []types.CreateOrderRequest = reqs[start:end]

		var infos []types.OrderInfo
		var errs []error
		var err error
		infos, errs, err = t.createBatchChunk(ctx, chunk)
		if err != nil {
			// Request-level failure: everything already accepted is returned,
			// the failure is joined so the caller can reconcile the rest.
			rowErrs = append(rowErrs, err)
			break
		}
		accepted = append(accepted, infos...)
		rowErrs = append(rowErrs, errs...)
	}

	return accepted, errors.Join(rowErrs...)
}

// createBatchChunk sends one batch create request (≤ MaxBatchSize orders).
func (t *TradingClient) createBatchChunk(ctx context.Context, chunk []types.CreateOrderRequest) ([]types.OrderInfo, []error, error) {
	var items []map[string]string = make([]map[string]string, 0, len(chunk))
	var symbols []string = make([]string, 0, len(chunk))
	var i int
	for i = 0; i < len(chunk); i++ {
		var fields map[string]string
		var err error
		fields, err = buildCreateOrderFields(chunk[i])
		if err != nil {
			return nil, nil, err
		}
		items = append(items, fields)
		symbols = append(symbols, chunk[i].Symbol)
	}

	var batchJSON []byte
	var err error
	batchJSON, err = codec.Marshal(items)
	if err != nil {
		return nil, nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.CreateBatchOrders: marshal batch", err)
	}

	var params url.Values = url.Values{}
	params.Set("batchOrders", string(batchJSON))

	var resp rest.Response
	var rateLimits map[string]string
	resp, rateLimits, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   endpointBatchOrders,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: len(chunk),
			Symbols:    uniqueSorted(symbols),
			Category:   string(aster.RateLimitCategoryPlace),
		},
	})
	if err != nil {
		return nil, nil, err
	}

	return t.parseBatchOrderRows(resp, rateLimits, "trading.CreateBatchOrders")
}

/*
ModifyBatchOrders modifies up to N LIMIT orders (price+quantity per row).
Chunking and partial-success semantics are identical to CreateBatchOrders.
*/
func (t *TradingClient) ModifyBatchOrders(ctx context.Context, reqs []types.ModifyOrderRequest) ([]types.OrderInfo, error) {
	if len(reqs) == 0 {
		return nil, nil
	}

	var accepted []types.OrderInfo = make([]types.OrderInfo, 0, len(reqs))
	var rowErrs []error

	var start int
	for start = 0; start < len(reqs); start += MaxBatchSize {
		var end int = start + MaxBatchSize
		if end > len(reqs) {
			end = len(reqs)
		}
		var chunk []types.ModifyOrderRequest = reqs[start:end]

		var infos []types.OrderInfo
		var errs []error
		var err error
		infos, errs, err = t.modifyBatchChunk(ctx, chunk)
		if err != nil {
			rowErrs = append(rowErrs, err)
			break
		}
		accepted = append(accepted, infos...)
		rowErrs = append(rowErrs, errs...)
	}

	return accepted, errors.Join(rowErrs...)
}

// modifyBatchChunk sends one batch modify request (≤ MaxBatchSize orders).
func (t *TradingClient) modifyBatchChunk(ctx context.Context, chunk []types.ModifyOrderRequest) ([]types.OrderInfo, []error, error) {
	var items []map[string]string = make([]map[string]string, 0, len(chunk))
	var symbols []string = make([]string, 0, len(chunk))
	var i int
	for i = 0; i < len(chunk); i++ {
		var fields map[string]string
		var err error
		fields, err = buildModifyOrderFields(chunk[i])
		if err != nil {
			return nil, nil, err
		}
		items = append(items, fields)
		symbols = append(symbols, chunk[i].Symbol)
	}

	var batchJSON []byte
	var err error
	batchJSON, err = codec.Marshal(items)
	if err != nil {
		return nil, nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.ModifyBatchOrders: marshal batch", err)
	}

	var params url.Values = url.Values{}
	params.Set("batchOrders", string(batchJSON))

	var resp rest.Response
	var rateLimits map[string]string
	resp, rateLimits, err = t.c.rest().Do(ctx, rest.Options{
		Method: "PUT",
		Path:   endpointBatchOrders,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: len(chunk),
			Symbols:    uniqueSorted(symbols),
			Category:   string(aster.RateLimitCategoryAmend),
		},
	})
	if err != nil {
		return nil, nil, err
	}

	return t.parseBatchOrderRows(resp, rateLimits, "trading.ModifyBatchOrders")
}

/*
CancelBatchOrders cancels up to N orders. Requests addressed by OrderID and by
OrigClientOrderID are partitioned into separate wire calls (the protocol
accepts either orderIdList or origClientOrderIdList per request, not both) and
chunked at MaxCancelBatchSize. Row rejections are joined into the returned
error; rows cancelled successfully are removed from the id-mapping cache.
*/
func (t *TradingClient) CancelBatchOrders(ctx context.Context, reqs []types.CancelOrderRequest) error {
	if len(reqs) == 0 {
		return nil
	}

	// Partition by addressing mode, preserving order inside each group.
	var byID []types.CancelOrderRequest
	var byClOrd []types.CancelOrderRequest
	var i int
	for i = 0; i < len(reqs); i++ {
		if reqs[i].Symbol == "" {
			return aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.CancelBatchOrders: Symbol is empty", nil)
		}
		if reqs[i].OrderID != 0 {
			byID = append(byID, reqs[i])
		} else if reqs[i].OrigClientOrderID != "" {
			byClOrd = append(byClOrd, reqs[i])
		} else {
			return aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.CancelBatchOrders: OrderID or OrigClientOrderID is required", nil)
		}
	}

	var rowErrs []error
	var err error
	err = t.cancelBatchGroup(ctx, byID, true, &rowErrs)
	if err != nil {
		return err
	}
	err = t.cancelBatchGroup(ctx, byClOrd, false, &rowErrs)
	if err != nil {
		return err
	}
	return errors.Join(rowErrs...)
}

// cancelBatchGroup cancels one addressing-mode group, chunked by symbol and
// MaxCancelBatchSize. The wire request is per-symbol, so requests are grouped
// by symbol first.
func (t *TradingClient) cancelBatchGroup(ctx context.Context, reqs []types.CancelOrderRequest, byID bool, rowErrs *[]error) error {
	if len(reqs) == 0 {
		return nil
	}

	// Group by symbol, preserving order.
	var symbolOrder []string
	var groups map[string][]types.CancelOrderRequest = map[string][]types.CancelOrderRequest{}
	var i int
	for i = 0; i < len(reqs); i++ {
		if _, ok := groups[reqs[i].Symbol]; !ok {
			symbolOrder = append(symbolOrder, reqs[i].Symbol)
		}
		groups[reqs[i].Symbol] = append(groups[reqs[i].Symbol], reqs[i])
	}

	var s int
	for s = 0; s < len(symbolOrder); s++ {
		var symbol string = symbolOrder[s]
		var group []types.CancelOrderRequest = groups[symbol]

		var start int
		for start = 0; start < len(group); start += MaxCancelBatchSize {
			var end int = start + MaxCancelBatchSize
			if end > len(group) {
				end = len(group)
			}
			var chunk []types.CancelOrderRequest = group[start:end]

			var params url.Values = url.Values{}
			params.Set("symbol", symbol)
			if byID {
				var ids []string = make([]string, 0, len(chunk))
				for i = 0; i < len(chunk); i++ {
					ids = append(ids, strconv.FormatInt(chunk[i].OrderID, 10))
				}
				// Wire format: [1234567,2345678] — no spaces.
				params.Set("orderIdList", "["+strings.Join(ids, ",")+"]")
			} else {
				var ids []string = make([]string, 0, len(chunk))
				for i = 0; i < len(chunk); i++ {
					ids = append(ids, `"`+chunk[i].OrigClientOrderID+`"`)
				}
				// Wire format: ["id1","id2"] — no spaces after commas.
				params.Set("origClientOrderIdList", "["+strings.Join(ids, ",")+"]")
			}

			var resp rest.Response
			var err error
			resp, _, err = t.c.rest().Do(ctx, rest.Options{
				Method: "DELETE",
				Path:   endpointBatchOrders,
				Params: params,
				Signed: true,
				Meta: rest.RequestMeta{
					OrderCount: len(chunk),
					Symbols:    []string{symbol},
					Category:   string(aster.RateLimitCategoryCancel),
				},
			})
			if err != nil {
				return err
			}

			var infos []types.OrderInfo
			var errs []error
			infos, errs, err = t.parseBatchOrderRows(resp, nil, "trading.CancelBatchOrders")
			if err != nil {
				return err
			}
			*rowErrs = append(*rowErrs, errs...)
			for i = 0; i < len(infos); i++ {
				t.forgetMapping(infos[i].ClientOrderID, infos[i].OrderID)
			}
		}
	}
	return nil
}

/*
CancelAllOrders cancels ALL active orders for the symbol
(DELETE /fapi/v3/allOpenOrders).
*/
func (t *TradingClient) CancelAllOrders(ctx context.Context, symbol string) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.CancelAllOrders: symbol is empty", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)

	var err error
	_, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "DELETE",
		Path:   endpointAllOpenOrders,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: 1,
			Symbols:    []string{symbol},
			Category:   string(aster.RateLimitCategoryCancel),
		},
	})
	if err != nil {
		return err
	}
	t.forgetSymbolMappings()
	return nil
}

/*
AutoCancelAllOpenOrders arms the exchange-side dead-man switch
(POST /fapi/v3/countdownCancelAll): all open orders of the symbol are
cancelled countdownTime milliseconds after the LAST heartbeat. Call
periodically (more often than countdownTime) to keep orders alive;
countdownTime = 0 disarms the timer.
*/
func (t *TradingClient) AutoCancelAllOpenOrders(ctx context.Context, symbol string, countdownTimeMs int64) error {
	if symbol == "" {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.AutoCancelAllOpenOrders: symbol is empty", nil)
	}
	if countdownTimeMs < 0 {
		return aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.AutoCancelAllOpenOrders: countdownTime is negative", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)
	params.Set("countdownTime", strconv.FormatInt(countdownTimeMs, 10))

	var err error
	_, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   endpointCountdownCancel,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryCancel),
		},
	})
	return err
}

/*
CancelForgottenOrders cancels active orders of the symbol older than
expirationSec seconds (client-side TTL sweep: GetOpenOrders + age filter +
CancelBatchOrders). Returns the orders that were selected for cancellation.
*/
func (t *TradingClient) CancelForgottenOrders(ctx context.Context, symbol string, expirationSec int) ([]types.OrderInfo, error) {
	if expirationSec <= 0 {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.CancelForgottenOrders: expiration must be positive", nil)
	}

	var open []types.OrderInfo
	var err error
	open, err = t.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	var cutoffMs int64 = time.Now().Add(-time.Duration(expirationSec) * time.Second).UnixMilli()
	var forgotten []types.OrderInfo
	var cancels []types.CancelOrderRequest
	var i int
	for i = 0; i < len(open); i++ {
		if open[i].CreatedTime != 0 && open[i].CreatedTime < cutoffMs {
			forgotten = append(forgotten, open[i])
			cancels = append(cancels, types.CancelOrderRequest{
				Symbol:  open[i].Symbol,
				OrderID: open[i].OrderID,
			})
		}
	}
	if len(cancels) == 0 {
		return nil, nil
	}

	err = t.CancelBatchOrders(ctx, cancels)
	if err != nil {
		return forgotten, err
	}
	return forgotten, nil
}

/*
GetOpenOrders returns all active orders of the symbol
(GET /fapi/v3/openOrders). The id-mapping cache is refreshed from the result.
*/
func (t *TradingClient) GetOpenOrders(ctx context.Context, symbol string) ([]types.OrderInfo, error) {
	if symbol == "" {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.GetOpenOrders: symbol is empty", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)

	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointOpenOrders,
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

	var raws []rawOrder
	if err = resp.Unmarshal(&raws); err != nil {
		return nil, aster.NewError(aster.ErrorKindUnknown, 0, "trading.GetOpenOrders: parse", err)
	}

	var infos []types.OrderInfo = make([]types.OrderInfo, 0, len(raws))
	var i int
	for i = 0; i < len(raws); i++ {
		var info types.OrderInfo = convertOrder(raws[i], nil)
		t.rememberMapping(info.ClientOrderID, info.OrderID, info.CreatedTime)
		infos = append(infos, info)
	}
	return infos, nil
}

/*
GetOrder queries a single order regardless of its status
(GET /fapi/v3/order). Addressed by OrderID or OrigClientOrderID.
*/
func (t *TradingClient) GetOrder(ctx context.Context, symbol string, orderID int64, origClientOrderID string) (types.OrderInfo, error) {
	return t.queryOrder(ctx, endpointOrder, "trading.GetOrder", symbol, orderID, origClientOrderID)
}

/*
GetOpenOrder queries a single CURRENTLY OPEN order (GET /fapi/v3/openOrder).
Returns an exchange error (-2013) when the order is not open any more.
*/
func (t *TradingClient) GetOpenOrder(ctx context.Context, symbol string, orderID int64, origClientOrderID string) (types.OrderInfo, error) {
	return t.queryOrder(ctx, endpointOpenOrder, "trading.GetOpenOrder", symbol, orderID, origClientOrderID)
}

// queryOrder — shared implementation of GetOrder / GetOpenOrder.
func (t *TradingClient) queryOrder(ctx context.Context, path, opName, symbol string, orderID int64, origClientOrderID string) (types.OrderInfo, error) {
	var info types.OrderInfo
	if symbol == "" {
		return info, aster.NewError(aster.ErrorKindInvalidRequest, 0, opName+": symbol is empty", nil)
	}
	if orderID == 0 && origClientOrderID == "" {
		return info, aster.NewError(aster.ErrorKindInvalidRequest, 0, opName+": orderID or origClientOrderID is required", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)
	if orderID != 0 {
		params.Set("orderId", strconv.FormatInt(orderID, 10))
	} else {
		params.Set("origClientOrderId", origClientOrderID)
	}

	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   path,
		Params: params,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{symbol},
			Category: string(aster.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return info, err
	}

	var raw rawOrder
	if err = resp.Unmarshal(&raw); err != nil {
		return info, aster.NewError(aster.ErrorKindUnknown, 0, opName+": parse", err)
	}
	info = convertOrder(raw, nil)
	t.rememberMapping(info.ClientOrderID, info.OrderID, info.CreatedTime)
	return info, nil
}

/*
GetAllOrders returns orders of the symbol in any status
(GET /fapi/v3/allOrders). Optional filters: fromOrderID (orderId >=),
startTimeMs/endTimeMs window, limit (default 500, max 1000). Zero values are
omitted.
*/
func (t *TradingClient) GetAllOrders(ctx context.Context, symbol string, fromOrderID int64, startTimeMs, endTimeMs int64, limit int) ([]types.OrderInfo, error) {
	if symbol == "" {
		return nil, aster.NewError(aster.ErrorKindInvalidRequest, 0, "trading.GetAllOrders: symbol is empty", nil)
	}

	var params url.Values = url.Values{}
	params.Set("symbol", symbol)
	if fromOrderID != 0 {
		params.Set("orderId", strconv.FormatInt(fromOrderID, 10))
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
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   endpointAllOrders,
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

	var raws []rawOrder
	if err = resp.Unmarshal(&raws); err != nil {
		return nil, aster.NewError(aster.ErrorKindUnknown, 0, "trading.GetAllOrders: parse", err)
	}

	var infos []types.OrderInfo = make([]types.OrderInfo, 0, len(raws))
	var i int
	for i = 0; i < len(raws); i++ {
		infos = append(infos, convertOrder(raws[i], nil))
	}
	return infos, nil
}

/*
SyncOrderMappings refreshes the ClientOrderID ↔ OrderID cache from the
exchange (GetOpenOrders side effect). Call after reconnects/restarts when the
local cache may be stale.
*/
func (t *TradingClient) SyncOrderMappings(ctx context.Context, symbol string) error {
	var err error
	_, err = t.GetOpenOrders(ctx, symbol)
	return err
}

// OrderIDByClientID returns the cached exchange id for a client order id.
func (t *TradingClient) OrderIDByClientID(clientOrderID string) (int64, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var id int64
	var ok bool
	id, ok = t.clOrdToOrd[clientOrderID]
	return id, ok
}

// ClientIDByOrderID returns the cached client order id for an exchange id.
func (t *TradingClient) ClientIDByOrderID(orderID int64) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var id string
	var ok bool
	id, ok = t.ordToClOrd[orderID]
	return id, ok
}

// rememberMapping stores a ClientOrderID ↔ OrderID pair.
func (t *TradingClient) rememberMapping(clientOrderID string, orderID int64, tsMs int64) {
	if clientOrderID == "" || orderID == 0 {
		return
	}
	t.mu.Lock()
	t.clOrdToOrd[clientOrderID] = orderID
	t.ordToClOrd[orderID] = clientOrderID
	t.createdAtMs[clientOrderID] = tsMs
	t.mu.Unlock()
}

// forgetMapping removes a pair by any known key.
func (t *TradingClient) forgetMapping(clientOrderID string, orderID int64) {
	t.mu.Lock()
	if clientOrderID == "" && orderID != 0 {
		clientOrderID = t.ordToClOrd[orderID]
	}
	if orderID == 0 && clientOrderID != "" {
		orderID = t.clOrdToOrd[clientOrderID]
	}
	delete(t.clOrdToOrd, clientOrderID)
	delete(t.ordToClOrd, orderID)
	delete(t.createdAtMs, clientOrderID)
	t.mu.Unlock()
}

// forgetSymbolMappings clears the whole cache. Used by CancelAllOrders: the
// cache is not partitioned by symbol, and a stale mapping is only a fallback —
// SyncOrderMappings restores the actual state.
func (t *TradingClient) forgetSymbolMappings() {
	t.mu.Lock()
	t.clOrdToOrd = map[string]int64{}
	t.ordToClOrd = map[int64]string{}
	t.createdAtMs = map[string]int64{}
	t.mu.Unlock()
}
