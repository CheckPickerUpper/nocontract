//// A shopper the shop holds no balance for.

/// The shared missing-shopper answer.
pub type MissingShopper {
  /// nocontract:status 404
  /// The shop holds no balance for this shopper.
  /// ergleam:code SH-1001
  ShopperNotFound(shopper_id: Int)
}
