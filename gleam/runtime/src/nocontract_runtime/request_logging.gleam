import gleam/json
import gleam/list
import gleam/option.{type Option, None, Some}
import wisp

/// Measures an answer's elapsed monotonic microseconds while preserving the answer.
@external(erlang, "timer", "tc")
pub fn measure_request(answer: fn() -> a) -> #(Int, a)

/// A finalized request answer. The route is its declared path template, or its actual
/// path when unmatched. Duration is elapsed microseconds from the server's monotonic clock.
pub type RequestLog {
  RequestLog(
    route: String,
    method: String,
    status: Int,
    code: Option(String),
    duration_us: Int,
  )
}

/// Emits one JSON request event through the application's standard Wisp logger.
pub fn log_request(entry: RequestLog) -> Nil {
  entry
  |> build_request_attributes
  |> json.object
  |> json.to_string
  |> wisp.log_info
}

fn build_request_attributes(entry: RequestLog) -> List(#(String, json.Json)) {
  let attributes = [
    #("route", json.string(entry.route)),
    #("method", json.string(entry.method)),
    #("status", json.int(entry.status)),
    #("duration_us", json.int(entry.duration_us)),
  ]
  case entry.code {
    Some(code) -> list.append(attributes, [#("code", json.string(code))])
    None -> attributes
  }
}
