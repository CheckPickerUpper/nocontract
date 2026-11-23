import gleeunit
import nocontract/server
import nocontract_runtime/bind_address
import nocontract_runtime/server_options

/// Runs the shop's test entry point.
pub fn main() -> Nil {
  gleeunit.main()
}

pub fn an_invalid_address_is_refused_before_start_test() {
  let assert Error(server_options.AddressRefused(cause: bind_address.InvalidLiteral(
    address: "not an IP address",
    reason: "einval",
  ))) = server.options(address: "not an IP address", port: 8080)
}
