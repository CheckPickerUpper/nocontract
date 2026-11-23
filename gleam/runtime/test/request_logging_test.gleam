import gleam/dynamic.{type Dynamic}
import gleam/dynamic/decode
import gleam/option
import nocontract_runtime/request_logging

@external(erlang, "nocontract_request_log_test_ffi", "capture")
fn capture(emit: fn() -> Nil) -> Dynamic

pub fn finalized_request_logs_reach_the_standard_logger_test() {
  let success =
    request_logging.RequestLog(
      route: "/shoppers/{shopper_id}",
      method: "GET",
      status: 200,
      code: option.None,
      duration_us: 17,
    )
  let assert Ok(success_output) =
    capture(fn() { request_logging.log_request(success) })
    |> decode.run(decode.string)
  assert success_output
    == "{\"route\":\"/shoppers/{shopper_id}\",\"method\":\"GET\",\"status\":200,\"duration_us\":17}\n"

  let refused =
    request_logging.RequestLog(
      route: "/shoppers/{shopper_id}",
      method: "GET",
      status: 404,
      code: option.Some("SH-1001"),
      duration_us: 2301,
    )
  let assert Ok(refused_output) =
    capture(fn() { request_logging.log_request(refused) })
    |> decode.run(decode.string)
  assert refused_output
    == "{\"route\":\"/shoppers/{shopper_id}\",\"method\":\"GET\",\"status\":404,\"duration_us\":2301,\"code\":\"SH-1001\"}\n"

  let unmatched =
    request_logging.RequestLog(
      route: "/missing\n\"path",
      method: "ODD\rMETHOD",
      status: 404,
      code: option.Some("NC-1069"),
      duration_us: 8,
    )
  let assert Ok(unmatched_output) =
    capture(fn() { request_logging.log_request(unmatched) })
    |> decode.run(decode.string)
  assert unmatched_output
    == "{\"route\":\"/missing\\n\\\"path\",\"method\":\"ODD\\rMETHOD\",\"status\":404,\"duration_us\":8,\"code\":\"NC-1069\"}\n"
}
