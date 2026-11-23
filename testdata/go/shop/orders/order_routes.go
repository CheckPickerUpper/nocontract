// Package orders takes shoppers' orders.
package orders

import (
	"context"
	"time"

	"github.com/samber/mo"
)

// Item is one line of an order: the product, quantity, and items included with it.
type Item struct {
	SKU      string `json:"sku"`
	Quantity uint8  `json:"quantity"`
	Included []Item `json:"included"`
}

// Courier is who delivers an order.
type Courier string

// The couriers who deliver orders.
const (
	CourierDrone Courier = "drone"
	CourierPost  Courier = "post"
)

// Order is what a shopper orders: its items, the note on a gift, the first and the last time it may
// be delivered, whether it is wrapped, the tip in each currency, and a note for each courier who
// may deliver it.
type Order struct {
	Items          []Item             `json:"items"`
	GiftNote       mo.Option[string]  `json:"gift_note"`
	DeliverBetween [2]time.Time       `json:"deliver_between"`
	Wrapped        bool               `json:"wrapped"`
	Tips           map[string]float64 `json:"tips"`
	CourierNotes   map[Courier]string `json:"courier_notes"`
}

// PlaceInput names the shopper and the store the order is placed with, and carries the order.
type PlaceInput struct {
	ShopperID int    `path:"shopper_id"`
	Store     uint16 `query:"store"`
	Body      Order
}

// PlaceOutcome is what placing an order ended as.
//
//sumtype:decl
type PlaceOutcome interface {
	isPlaceOutcome()
}

// OrderPlaced is an order the shop took, as it read it.
//
//ergo:not-an-error
//nocontract:status 201
type OrderPlaced struct {
	Order Order `json:"order"`
}

func (OrderPlaced) isPlaceOutcome() {}

// Place takes a shopper's order.
//
//nocontract:route POST /shoppers/{shopper_id}/orders
func Place(_ context.Context, input PlaceInput) PlaceOutcome {
	return OrderPlaced{Order: input.Body}
}

// OrderCountInput reads the required order count header.
type OrderCountInput struct {
	OrderCount int64 `header:"X-Order-Count"`
}

// OrderCountOutcome is the answer to reading the order count header.
//
//sumtype:decl
type OrderCountOutcome interface {
	isOrderCountOutcome()
}

// OrderCountRead is the order count the request declared.
//
//ergo:not-an-error
//nocontract:status 200
type OrderCountRead struct {
	OrderCount int64 `json:"order_count"`
}

func (OrderCountRead) isOrderCountOutcome() {}

// ReadOrderCount answers with the typed order count header.
//
//nocontract:route GET /headers/order-count
func ReadOrderCount(_ context.Context, input OrderCountInput) OrderCountOutcome {
	return OrderCountRead{OrderCount: input.OrderCount}
}
