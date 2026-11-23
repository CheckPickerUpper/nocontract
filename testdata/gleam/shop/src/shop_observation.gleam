import gleam/json
import gleam/list
import gleam/option
import nocontract_runtime/request_layers
import wisp

/// Records each phase of one shop observer through the application logger.
pub fn layer(name: String) -> request_layers.Layer {
  fn(visit) {
    emit(name, "before", visit, [])
    fn(answered) { emit(name, "after", visit, answer_fields(answered)) }
  }
}

fn answer_fields(
  answered: request_layers.Answered,
) -> List(#(String, json.Json)) {
  let fields = [#("status", json.int(answered.status))]
  case answered.code {
    option.Some(code) -> list.append(fields, [#("code", json.string(code))])
    option.None -> fields
  }
}

fn emit(
  layer: String,
  phase: String,
  visit: request_layers.Visit,
  answer: List(#(String, json.Json)),
) -> Nil {
  [
    #("layer", json.string(layer)),
    #("phase", json.string(phase)),
    #("method", json.string(visit.method)),
    #("path", json.string(visit.path)),
    #("route", json.string(visit.route)),
    ..answer
  ]
  |> json.object
  |> json.to_string
  |> wisp.log_info
}
