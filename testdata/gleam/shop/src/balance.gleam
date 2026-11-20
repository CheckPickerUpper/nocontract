//// Reads shoppers' balances and takes top-up requests.

import gleam/option.{None, Some}
import shop/money.{type Currency, type Money, Cad, Money, Usd}
import shop/read_outcome.{type ReadOutcome}
import shop/scalars.{Instant}
import shop/shopper_errors
import shop/top_up_outcome.{type TopUpOutcome}

/// Names the shopper and the currency to read the balance in.
pub type ReadInput {
  ReadInput(shopper_id: Int, currency: Currency)
}

/// Names the shopper and carries the amount to add.
pub type TopUpInput {
  TopUpInput(shopper_id: Int, body: TopUpBody)
}

/// The amount the shopper asks to add.
pub type TopUpBody {
  TopUpBody(amount: Money)
}

/// nocontract:route GET /shoppers/{shopper_id}/balance
/// nocontract:guard shopperauth.check read-balance
/// Reads one shopper's balance after the shopper guard admits the request.
pub fn read_balance(input: ReadInput) -> ReadOutcome {
  case input.shopper_id, input.currency {
    7, Cad ->
      read_outcome.BalanceRead(
        balance: Money(cents: 1250, currency: input.currency),
        overdraft: None,
        changed: Instant(value: "2026-11-14T12:00:00Z"),
        note: Some("first top-up"),
      )
    7, Usd -> read_outcome.BalanceNotOpened(currency: input.currency)
    _, Cad | _, Usd ->
      read_outcome.MissingShopper(shopper_errors.ShopperNotFound(
        shopper_id: input.shopper_id,
      ))
  }
}

/// nocontract:route POST /shoppers/{shopper_id}/top-ups
/// Asks for an amount to be added to a shopper's balance.
pub fn top_up(input: TopUpInput) -> TopUpOutcome {
  case input.body.amount.cents > 0 {
    True ->
      top_up_outcome.TopUpQueued(reference: currency_text(
        input.body.amount.currency,
      ))
    False ->
      top_up_outcome.TopUpRefused(reasons: ["the amount is not above zero"])
  }
}

fn currency_text(currency: Currency) -> String {
  case currency {
    Cad -> "CAD"
    Usd -> "USD"
  }
}
