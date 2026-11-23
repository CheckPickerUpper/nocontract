import gleam/list
import gleam/option.{type Option}

/// The request method, actual path, and matched route template or unmatched path.
pub type Visit {
  Visit(method: String, path: String, route: String)
}

/// The status and optional permanent code of the answer the listener returns.
pub type Answered {
  Answered(status: Int, code: Option(String))
}

/// Observes a request on entry and returns its observer of the finalized answer.
pub type Layer =
  fn(Visit) -> fn(Answered) -> Nil

/// Enters layers in declaration order and returns their reverse-order answer observer.
pub fn enter(layers: List(Layer), visit: Visit) -> fn(Answered) -> Nil {
  let after =
    list.fold(layers, [], fn(after, layer) { [layer(visit), ..after] })
  fn(answered) { list.each(after, fn(finish) { finish(answered) }) }
}
