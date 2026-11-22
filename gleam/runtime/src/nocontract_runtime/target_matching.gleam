import gleam/dict.{type Dict}
import gleam/list
import gleam/option.{type Option, None, Some}
import gleam/order
import gleam/result
import gleam/string
import gleam/uri

/// One segment of a declared path.
pub type Segment {
  PathText(text: String)
  PathParameter(name: String)
}

/// A generated operation with its method and path from the shared description.
pub type Route(operation) {
  Route(operation: operation, method: String, path: List(Segment))
}

/// How a request's method and escaped path match the declared routes.
pub type Selection(operation) {
  Selected(operation: operation, parameters: Dict(String, String))
  UndeclaredMethod(allowed: List(String))
  UnknownPath(path: String)
  UnreadablePath(segment: String)
}

type Candidate(operation) {
  Candidate(route: Route(operation), parameters: Dict(String, String))
}

/// Matches decoded path segments, preferring literal segments over parameters.
pub fn match_target(
  routes: List(Route(a)),
  method method: String,
  path path: String,
) -> Selection(a) {
  let segments = path |> string.drop_start(1) |> string.split("/")
  case
    list.try_map(segments, fn(segment) {
      segment |> uri.percent_decode |> result.replace_error(segment)
    })
  {
    Error(segment) -> UnreadablePath(segment:)
    Ok(segments) -> {
      let candidates =
        routes
        |> list.filter_map(fn(route) {
          case match_segments(route.path, segments, dict.new()) {
            None -> Error(Nil)
            Some(parameters) -> Ok(Candidate(route:, parameters:))
          }
        })
        |> list.sort(fn(left, right) {
          precedence(left.route.path, right.route.path)
        })
      choose(candidates, method, path)
    }
  }
}

fn choose(
  candidates: List(Candidate(a)),
  method: String,
  path: String,
) -> Selection(a) {
  case candidates {
    [] -> UnknownPath(path: decoded_path(path))
    [first, ..] -> {
      let matches =
        list.take_while(candidates, fn(candidate) {
          precedence(first.route.path, candidate.route.path) == order.Eq
        })
      case
        list.find(matches, fn(candidate) { candidate.route.method == method })
      {
        Ok(matched) ->
          Selected(
            operation: matched.route.operation,
            parameters: matched.parameters,
          )
        Error(Nil) ->
          UndeclaredMethod(
            allowed: matches
            |> list.map(fn(candidate) { candidate.route.method })
            |> list.sort(string.compare)
            |> list.unique,
          )
      }
    }
  }
}

fn decoded_path(path: String) -> String {
  case uri.percent_decode(path) {
    Ok(path) -> path
    Error(Nil) -> path
  }
}

fn match_segments(
  declared: List(Segment),
  segments: List(String),
  parameters: Dict(String, String),
) -> Option(Dict(String, String)) {
  case declared, segments {
    [], [] -> Some(parameters)
    [PathText(text:), ..declared], [segment, ..segments] if text == segment ->
      match_segments(declared, segments, parameters)
    [PathParameter(name:), ..declared], [segment, ..segments] if segment != "" ->
      match_segments(declared, segments, dict.insert(parameters, name, segment))
    [], [_, ..] | [PathText(..), ..], _ | [PathParameter(..), ..], _ -> None
  }
}

fn precedence(left: List(Segment), right: List(Segment)) -> order.Order {
  case left, right {
    [], [] -> order.Eq
    [PathText(..), ..], [PathParameter(..), ..] -> order.Lt
    [PathParameter(..), ..], [PathText(..), ..] -> order.Gt
    [PathText(..), ..left], [PathText(..), ..right]
    | [PathParameter(..), ..left], [PathParameter(..), ..right]
    -> precedence(left, right)
    [], [_, ..] -> order.Lt
    [_, ..], [] -> order.Gt
  }
}

/// Reads query entries independently, as Go's URL reader does for malformed entries.
pub fn read_query(query: String) -> List(#(String, String)) {
  query
  |> string.split("&")
  |> list.flat_map(fn(part) {
    case uri.parse_query(part) {
      Ok(fields) -> fields
      Error(Nil) -> []
    }
  })
}
