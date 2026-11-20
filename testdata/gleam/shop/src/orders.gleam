//// Takes shoppers' orders.

import gleam/dict.{type Dict}
import gleam/option.{type Option}
import shop/scalars.{type Instant, type UInt16, type UInt8}

/// One line of an order.
pub type Item {
  Item(sku: String, quantity: UInt8, included: List(Item))
}

/// Who delivers an order.
pub type Courier {
  /// nocontract:literal drone
  CourierDrone
  /// nocontract:literal post
  CourierPost
}

/// What a shopper orders and how it may be delivered.
pub type Order {
  Order(
    items: List(Item),
    gift_note: Option(String),
    deliver_between: #(Instant, Instant),
    wrapped: Bool,
    tips: Dict(String, Float),
    courier_notes: Dict(Courier, String),
  )
}

/// Names the shopper and store and carries the order.
pub type PlaceInput {
  PlaceInput(shopper_id: Int, store: UInt16, body: Order)
}

/// What placing an order ended as.
pub type PlaceOutcome {
  /// nocontract:status 201
  /// An order the shop took, as it read it.
  /// ergleam:not-an-error
  OrderPlaced(order: Order)
}

/// nocontract:route POST /shoppers/{shopper_id}/orders
/// Takes a shopper's order.
pub fn place(input: PlaceInput) -> PlaceOutcome {
  OrderPlaced(order: input.body)
}
