//// The outcomes of reading a shopper's balance.

import gleam/option.{type Option}
import shop/money.{type Currency, type Money}
import shop/scalars.{type Instant}
import shop/shopper_errors.{type MissingShopper}

/// What reading a balance ended as.
pub type ReadOutcome {
  /// nocontract:status 200
  /// The shopper's balance and the last change.
  /// ergleam:not-an-error
  BalanceRead(
    balance: Money,
    overdraft: Option(Money),
    changed: Instant,
    note: Option(String),
  )
  MissingShopper(MissingShopper)
  /// nocontract:status 404
  /// The shopper holds no balance in this currency.
  /// ergleam:code SH-1003
  BalanceNotOpened(currency: Currency)
}
