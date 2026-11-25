import nocontract_runtime/server_options

/// The request deadline SHOP_TEST_REQUEST_DEADLINE names, or a shop started without one.
type TestDeadline {
  NoTestDeadline
  TestDeadline(milliseconds: Int)
}

// The foreign function constructs this module's TestDeadline from the environment.
@external(erlang, "nocontract_shop_request_deadline_ffi", "read")
fn read_test_deadline() -> TestDeadline

/// Applies the deadline SHOP_TEST_REQUEST_DEADLINE names in milliseconds, and leaves the shop
/// without a deadline when it is unset.
pub fn with_test_request_deadline(
  options: server_options.StartOptions,
) -> server_options.StartOptions {
  case read_test_deadline() {
    NoTestDeadline -> options
    TestDeadline(milliseconds:) -> {
      let assert Ok(options) =
        server_options.with_request_deadline(options:, milliseconds:)
      options
    }
  }
}
