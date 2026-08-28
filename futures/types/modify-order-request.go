/*
FILE: futures/types/modify-order-request.go

DESCRIPTION:
Order modification request for PUT /fapi/v3/order. Aster modification rules:
  - only LIMIT orders can be modified;
  - BOTH Quantity and Price must be sent;
  - the order is addressed by OrderID or OrigClientOrderID (OrderID wins when
    both are set);
  - a modification that violates PRICE_FILTER/LOT_SIZE is rejected and the
    original order remains.
*/

package types

import "github.com/shopspring/decimal"

// ModifyOrderRequest — parameters for modifying an order.
type ModifyOrderRequest struct {
	// Symbol — trading pair. Required.
	Symbol string
	// OrderID — exchange order id. Either OrderID or OrigClientOrderID is
	// required; OrderID takes precedence.
	OrderID int64
	// OrigClientOrderID — user-defined order id of the order to modify.
	OrigClientOrderID string
	// Quantity — new order quantity. Required.
	Quantity decimal.Decimal
	// Price — new order price. Required.
	Price decimal.Decimal
}
