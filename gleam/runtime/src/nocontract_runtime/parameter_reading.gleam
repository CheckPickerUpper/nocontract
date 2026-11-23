import gleam/dynamic
import gleam/dynamic/decode
import gleam/int
import gleam/json
import gleam/list
import gleam/string
import nocontract_runtime/value_reading as values

/// One supported textual parameter shape from the shared description.
pub type ParameterSchema {
  TextParameter
  IntegerParameter(least: Int, most: Int, parser: String)
  TruthParameter
  LiteralParameter(values: List(String))
}

/// Why a textual parameter could not be read as the declared type.
pub type ParameterRefusal {
  ParameterMissing(name: String)
  HeaderMissing(name: String)
  DecimalNotCanonical(read: String)
  IntegerOutside(read: String, least: Int, most: Int, parser: String)
  TruthUnreadable(read: String)
  LiteralUnknown(read: String, values: List(String))
  NativeMismatch(read: String, errors: List(decode.DecodeError))
}

/// Reads present text into the native constructor's declared scalar shape.
pub fn read(
  text: String,
  schema: ParameterSchema,
  decoder: decode.Decoder(a),
) -> Result(a, ParameterRefusal) {
  case parse(text, schema) {
    Error(cause) -> Error(cause)
    Ok(native) -> {
      case decode.run(native, decoder) {
        Ok(value) -> Ok(value)
        Error(errors) -> Error(NativeMismatch(read: text, errors:))
      }
    }
  }
}

/// Reads the first occurrence of a query parameter, refusing a missing entry.
pub fn read_query(
  query: List(#(String, String)),
  name: String,
  schema: ParameterSchema,
  decoder: decode.Decoder(a),
) -> Result(a, ParameterRefusal) {
  case list.key_find(query, name) {
    Ok(text) -> read(text, schema, decoder)
    Error(Nil) -> Error(ParameterMissing(name:))
  }
}

/// Combines matching header lines in wire order before reading the declared scalar.
pub fn read_header(
  headers: List(#(String, String)),
  name: String,
  schema: ParameterSchema,
  decoder: decode.Decoder(a),
) -> Result(a, ParameterRefusal) {
  let values =
    headers
    |> list.filter(fn(header) { string.lowercase(header.0) == name })
    |> list.map(fn(header) { header.1 })
  case values {
    [] -> Error(HeaderMissing(name:))
    [_, ..] -> read(string.join(values, ", "), schema, decoder)
  }
}

fn parse(
  text: String,
  schema: ParameterSchema,
) -> Result(dynamic.Dynamic, ParameterRefusal) {
  case schema {
    TextParameter -> Ok(dynamic.string(text))
    LiteralParameter(values:) -> {
      case list.contains(values, text) {
        True -> Ok(dynamic.string(text))
        False -> Error(LiteralUnknown(read: text, values:))
      }
    }
    TruthParameter -> {
      case text {
        "true" | "True" | "TRUE" | "1" | "t" | "T" -> Ok(dynamic.bool(True))
        "false" | "False" | "FALSE" | "0" | "f" | "F" -> Ok(dynamic.bool(False))
        _ -> Error(TruthUnreadable(read: text))
      }
    }
    IntegerParameter(least:, most:, parser:) -> {
      case values.parse_decimal(text) {
        Error(values.NonCanonicalDecimal(..)) ->
          Error(DecimalNotCanonical(read: text))
        Ok(integer) if integer < least || integer > most ->
          Error(IntegerOutside(read: text, least:, most:, parser:))
        Ok(integer) -> Ok(dynamic.int(integer))
      }
    }
  }
}

/// The request refusal's reason, including the exact text that failed parsing.
pub fn describe(refusal: ParameterRefusal) -> String {
  case refusal {
    ParameterMissing(..) -> "the query parameter is missing"
    HeaderMissing(..) -> "the header is missing"
    DecimalNotCanonical(read:) ->
      json.to_string(json.string(read)) <> " " <> values.decimal_requirement()
    IntegerOutside(read:, parser:, ..) ->
      "strconv."
      <> parser
      <> ": parsing "
      <> json.to_string(json.string(read))
      <> ": value out of range"
    TruthUnreadable(read:) ->
      "strconv.ParseBool: parsing "
      <> json.to_string(json.string(read))
      <> ": invalid syntax"
    LiteralUnknown(read:, ..) ->
      json.to_string(json.string(read))
      <> " is not one of the values this field accepts."
    NativeMismatch(read:, errors:) ->
      "native parameter value "
      <> read
      <> ": "
      <> int.to_string(list.length(errors))
      <> " decoder errors"
  }
}
