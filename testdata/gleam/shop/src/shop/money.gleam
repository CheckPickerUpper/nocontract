//// Amounts in the currencies the shop accepts.

/// A currency a balance is held in.
pub type Currency {
  /// nocontract:literal CAD
  Cad
  /// nocontract:literal USD
  Usd
}

/// An amount in one currency.
pub type Money {
  Money(cents: Int, currency: Currency)
}
