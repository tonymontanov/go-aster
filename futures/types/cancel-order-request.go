/*
FILE: futures/types/cancel-order-request.go

DESCRIPTION:
Order cancellation request for DELETE /fapi/v3/order. The order is addressed
by OrderID or OrigClientOrderID (OrderID takes precedence when both are set).
*/

package types

// CancelOrderRequest — parameters for cancelling an order.
type CancelOrderRequest struct {
	// Symbol — trading pair. Required.
	Symbol string
	// OrderID — exchange order id. Either OrderID or OrigClientOrderID is
	// required.
	OrderID int64
	// OrigClientOrderID — user-defined order id of the order to cancel.
	OrigClientOrderID string
}
