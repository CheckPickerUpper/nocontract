import gleam/erlang/process
import nocontract/server
import shop_observation
import wisp

/// Starts the generated shop server on a port assigned by the kernel.
pub fn main() -> Nil {
  wisp.configure_logger()
  let assert Ok(options) = server.options(address: "127.0.0.1", port: 0)
  let options =
    server.with_layers(options, [
      shop_observation.layer("outer"),
      shop_observation.layer("inner"),
    ])
  let assert Ok(_) = server.start(options)
  process.sleep_forever()
}
