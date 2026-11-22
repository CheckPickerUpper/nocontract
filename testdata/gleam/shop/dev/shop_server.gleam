import gleam/erlang/process
import nocontract/server

/// Starts the generated shop server on a port assigned by the kernel.
pub fn main() -> Nil {
  let assert Ok(options) = server.options(address: "127.0.0.1", port: 0)
  let assert Ok(_) = server.start(options)
  process.sleep_forever()
}
