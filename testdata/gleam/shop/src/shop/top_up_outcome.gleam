//// The outcomes of asking for a top-up.

import shop/shopper_errors.{type MissingShopper}

/// What asking for a top-up ended as.
pub type TopUpOutcome {
  /// nocontract:status 202
  /// A top-up the shop will apply under its reference.
  /// ergleam:not-an-error
  TopUpQueued(reference: String)
  /// nocontract:status 409
  /// A top-up the shop will not apply, with every reason.
  /// ergleam:code SH-1002
  TopUpRefused(reasons: List(String))
  MissingShopper(MissingShopper)
}
