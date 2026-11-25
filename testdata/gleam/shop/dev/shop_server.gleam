import gleam/erlang/process
import nocontract/server
import nocontract_runtime/server_options
import shop_observation
import shop_request_deadline
import wisp

/// Starts the generated shop server on a port assigned by the kernel.
pub fn main() -> Nil {
  wisp.configure_logger()
  let assert Ok(options) = server.options(address: "127.0.0.1", port: 0)
  let options =
    server_options.with_layers(options:, layers: [
      shop_observation.layer("outer"),
      shop_observation.layer("inner"),
    ])
  let options = shop_request_deadline.with_test_request_deadline(options)
  let assert Ok(_) = server.start(options)
  process.sleep_forever()
}
