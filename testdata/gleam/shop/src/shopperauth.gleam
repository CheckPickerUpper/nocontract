//// Admits requests naming a shopper.

import gleam/dict.{type Dict}
import gleam/option.{type Option, None, Some}

/// Why a shopper's request is refused.
pub type Refusal {
  /// nocontract:status 401
  /// A request without a shopper for a route that needs one.
  /// ergleam:code SH-1101
  NotSignedIn(scope: String)
}

/// Admits a request naming a shopper and refuses one without the header.
pub fn check(scope: String, headers: Dict(String, String)) -> Option(Refusal) {
  case dict.get(headers, "x-shopper") {
    Ok("") | Error(Nil) -> Some(NotSignedIn(scope:))
    Ok(_) -> None
  }
}
