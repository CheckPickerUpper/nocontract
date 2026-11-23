//// Verifies credentials before admitting requests to shopper routes.

import gleam/dict.{type Dict}
import gleam/option.{type Option, None, Some}
import nocontract_runtime/sign_in_check

/// Why a shopper's request is refused.
pub type Refusal {
  /// nocontract:status 401
  /// A request without a shopper for a route that needs one.
  /// ergleam:code SH-1101
  NotSignedIn(scope: String)
  /// nocontract:status 401
  /// A request with an unrecognized shopper credential.
  /// ergleam:code SH-1102
  ShopperCredentialRejected(scope: String)
}

/// Admits the example credential shopper-7 and refuses missing or unrecognized credentials.
pub fn check(scope: String, headers: Dict(String, String)) -> Option(Refusal) {
  sign_in_check.check(
    headers,
    "x-shopper",
    fn() { NotSignedIn(scope:) },
    fn(credential) {
      case credential == "shopper-7" {
        True -> None
        False -> Some(ShopperCredentialRejected(scope:))
      }
    },
  )
}
