package orders

// Instant is an RFC 3339 timestamp.
//nocontract:instant
Instant :: distinct string

// Item is one line of an order.
Item :: struct #all_or_none {
	sku:      string,
	quantity: u8,
}

// Courier is who delivers an order.
Courier :: enum {
	drone,
	post,
}

// Order is what a shopper orders and how it is delivered.
Order :: struct #all_or_none {
	items:           []Item,
	gift_note:       Maybe(string),
	deliver_between: [2]Instant,
	wrapped:         bool,
	tips:            map[string]f64,
	courier_notes:   map[Courier]string,
}

// PlaceInput names the shopper and store and carries the order.
PlaceInput :: struct #all_or_none {
	shopper_id: i64,
	store:      u16,
	body:       Order,
}

// OrderPlaced is an order the shop took, as it read it.
//nocontract:status 201
@(not_an_error)
OrderPlaced :: struct #all_or_none {
	order: Order,
}

// PlaceOutcome is what placing an order ends as.
PlaceOutcome :: union {
	OrderPlaced,
}

// Place takes a shopper's order.
//nocontract:route POST /shoppers/{shopper_id}/orders
@(require_results)
Place :: proc(input: PlaceInput) -> PlaceOutcome {
	return OrderPlaced{order = input.body}
}
