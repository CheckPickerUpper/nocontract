import gleam/bit_array
import gleam/dict.{type Dict}
import gleam/dynamic/decode
import gleam/result
import gleam/string
import nocontract_runtime/parameter_reading as parameter
import nocontract_runtime/value_reading as value
import wisp

/// The boundary and full cause of a request the generated route could not read.
pub type RequestRefusal {
  PathRefused(name: String, cause: parameter.ParameterRefusal)
  QueryRefused(name: String, cause: parameter.ParameterRefusal)
  BodyRefused(cause: value.ValueRefusal)
  BodyNotReceived(path: String)
  BodyNotUtf8(path: String, content: BitArray)
  BodyNativeMismatch(path: String, cause: value.NativeDecodeRefusal)
}

/// Reads the declared path parameter and decodes its compiler-specific constructor.
pub fn read_path(
  parameters: Dict(String, String),
  name: String,
  schema: parameter.ParameterSchema,
  decoder: decode.Decoder(a),
) -> Result(a, RequestRefusal) {
  case dict.get(parameters, name) {
    Ok(text) ->
      parameter.read(text, schema, decoder)
      |> result.map_error(PathRefused(name:, cause: _))
    Error(Nil) ->
      Error(PathRefused(name:, cause: parameter.ParameterMissing(name:)))
  }
}

/// Reads the first query value for the declared parameter.
pub fn read_query(
  query: List(#(String, String)),
  name: String,
  schema: parameter.ParameterSchema,
  decoder: decode.Decoder(a),
) -> Result(a, RequestRefusal) {
  parameter.read_query(query, name, schema, decoder)
  |> result.map_error(QueryRefused(name:, cause: _))
}

/// Reads JSON with Wisp, parses its exact contract and constructs the native value.
pub fn read_body(
  request: wisp.Request,
  schema: value.Schema,
  records: Dict(String, value.Schema),
  decoder: decode.Decoder(a),
) -> Result(a, RequestRefusal) {
  case wisp.read_body_bits(request) {
    Error(Nil) -> Error(BodyNotReceived(path: request.path))
    Ok(content) -> {
      case bit_array.to_string(content) {
        Error(Nil) -> Error(BodyNotUtf8(path: request.path, content:))
        Ok(content) -> {
          use prepared <- result.try(
            value.read_body(content, schema, records)
            |> result.map_error(BodyRefused(cause: _)),
          )
          value.decode_value(prepared, decoder)
          |> result.map_error(BodyNativeMismatch(path: request.path, cause: _))
        }
      }
    }
  }
}

/// The fields the shared RequestRefused response publishes.
pub fn describe(refusal: RequestRefusal) -> #(String, String, String) {
  case refusal {
    PathRefused(name:, cause:) -> #("path", name, parameter.describe(cause))
    QueryRefused(name:, cause:) -> #("query", name, parameter.describe(cause))
    BodyRefused(cause:) -> {
      let #(name, reason) = value.describe(cause)
      #("body", name, reason)
    }
    BodyNotReceived(path:) -> #(
      "body",
      "body",
      "The body could not be read at " <> path <> ".",
    )
    BodyNotUtf8(path:, content:) -> #(
      "body",
      "body",
      "The body at " <> path <> " is not UTF-8: " <> string.inspect(content),
    )
    BodyNativeMismatch(path:, cause:) -> #(
      "body",
      "body",
      "Native body decoder at " <> path <> ": " <> string.inspect(cause),
    )
  }
}
