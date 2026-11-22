import gleam/dict.{type Dict}
import gleam/dynamic
import gleam/dynamic/decode
import gleam/int
import gleam/json
import gleam/list
import gleam/result
import gleam/string
import gleam/time/duration
import gleam/time/timestamp
import nocontract_runtime/json_reading as source

/// How an integer is represented in JSON.
pub type IntegerEncoding {
  DecimalString
  JsonNumber
}

/// The shared description's accepted JSON shapes.
pub type Schema {
  TextSchema
  IntegerSchema(least: Int, most: Int, encoding: IntegerEncoding)
  FiniteSchema
  TruthSchema
  InstantSchema
  LiteralsSchema(values: List(String))
  ListSchema(item: Schema)
  FixedListSchema(item: Schema, length: Int)
  DictionarySchema(keys: DictionaryKeys, item: Schema)
  NullableSchema(inner: Schema)
  RecordSchema(fields: List(#(String, Schema)))
  ReferenceSchema(name: String)
}

/// The names accepted by a JSON dictionary.
pub type DictionaryKeys {
  AnyKey
  LiteralKeys(values: List(String))
}

/// A checked value for the native decoder and the canonical wire encoder.
pub opaque type PreparedValue {
  PreparedValue(native: dynamic.Dynamic, wire: json.Json)
}

/// The exact value and boundary that refused a JSON field.
pub type ValueRefusal {
  JsonUnreadable(cause: source.JsonRefusal)
  MemberMissing(path: String)
  MemberRepeated(path: String)
  MemberUnknown(path: String)
  ValueMistyped(path: String, want: String, read: source.Value)
  LiteralOutside(path: String, read: String, values: List(String))
  KeyOutside(path: String, read: String, keys: List(String))
  NumberNotWhole(path: String, read: String)
  NumberOutside(path: String, read: String, least: Int, most: Int)
  DecimalNotCanonical(path: String, read: String)
  InstantUnreadable(path: String, read: String)
  ListLengthWrong(path: String, length: Int, read: Int)
  ReferenceMissing(path: String, name: String)
}

/// The compiler-specific decoder and the parsed contract disagreed.
pub type NativeDecodeRefusal {
  NativeDecodeRefused(errors: List(decode.DecodeError))
}

/// Decodes the compiler-specific native value after the contract parsed it.
pub fn decode_value(
  value: PreparedValue,
  decoder: decode.Decoder(a),
) -> Result(a, NativeDecodeRefusal) {
  value.native
  |> decode.run(decoder)
  |> result.map_error(NativeDecodeRefused(errors: _))
}

/// Encodes a parsed value using its canonical wire representation.
pub fn encode_value(value: PreparedValue) -> json.Json {
  value.wire
}

/// Parses one complete JSON body against the assembled description.
pub fn read_body(
  content: String,
  schema: Schema,
  records: Dict(String, Schema),
) -> Result(PreparedValue, ValueRefusal) {
  use value <- result.try(
    source.read(content) |> result.map_error(JsonUnreadable(cause: _)),
  )
  read_value(value, schema, records, "body")
}

/// Parses a JSON value against its contract at the named location.
fn read_value(
  value: source.Value,
  schema: Schema,
  records: Dict(String, Schema),
  path: String,
) -> Result(PreparedValue, ValueRefusal) {
  case schema, value {
    TextSchema, source.TextValue(text:) ->
      Ok(PreparedValue(native: dynamic.string(text), wire: json.string(text)))
    IntegerSchema(least:, most:, encoding: DecimalString),
      source.TextValue(text:)
    -> {
      use integer <- result.try(
        parse_decimal(text)
        |> result.map_error(fn(_) { DecimalNotCanonical(path:, read: text) }),
      )
      bounded(
        integer,
        text: source.text(value),
        least:,
        most:,
        encoding: DecimalString,
        path:,
      )
    }
    IntegerSchema(least:, most:, encoding: JsonNumber),
      source.NumberValue(text:)
    -> {
      use integer <- result.try(
        int.parse(text)
        |> result.replace_error(NumberNotWhole(path:, read: text)),
      )
      bounded(integer, text:, least:, most:, encoding: JsonNumber, path:)
    }
    FiniteSchema, source.NumberValue(text:) -> {
      case json.parse(from: text, using: decode.float) {
        Ok(number) ->
          Ok(PreparedValue(
            native: dynamic.float(number),
            wire: json.float(number),
          ))
        Error(_) ->
          Error(ValueMistyped(path:, want: "a finite number", read: value))
      }
    }
    TruthSchema, source.BooleanValue(value: boolean) ->
      Ok(PreparedValue(native: dynamic.bool(boolean), wire: json.bool(boolean)))
    InstantSchema, source.TextValue(text:) -> {
      case timestamp.parse_rfc3339(text) {
        Ok(instant) -> {
          let text = timestamp.to_rfc3339(instant, duration.seconds(0))
          Ok(PreparedValue(
            native: dynamic.string(text),
            wire: json.string(text),
          ))
        }
        Error(Nil) -> Error(InstantUnreadable(path:, read: text))
      }
    }
    LiteralsSchema(values:), source.TextValue(text:) -> {
      case list.contains(values, text) {
        True ->
          Ok(PreparedValue(
            native: dynamic.string(text),
            wire: json.string(text),
          ))
        False -> Error(LiteralOutside(path:, read: text, values:))
      }
    }
    ListSchema(item:), source.ArrayValue(items:) ->
      read_items(items, item, records, path)
    FixedListSchema(item:, length:), source.ArrayValue(items:) -> {
      case list.length(items) {
        read if read == length -> read_items(items, item, records, path)
        read -> Error(ListLengthWrong(path:, length:, read:))
      }
    }
    DictionarySchema(keys:, item:), source.ObjectValue(fields:) ->
      read_dictionary(fields, keys, item, records, path, dict.new())
    NullableSchema(..), source.NullValue ->
      Ok(PreparedValue(native: dynamic.nil(), wire: json.null()))
    NullableSchema(inner:), _ -> read_value(value, inner, records, path)
    RecordSchema(fields:), source.ObjectValue(fields: written) ->
      read_record(written, fields, records, path, dict.new())
    ReferenceSchema(name:), _ -> {
      case dict.get(records, name) {
        Ok(schema) -> read_value(value, schema, records, path)
        Error(Nil) -> Error(ReferenceMissing(path:, name:))
      }
    }
    TextSchema, _
    | IntegerSchema(..), _
    | FiniteSchema, _
    | TruthSchema, _
    | InstantSchema, _
    | LiteralsSchema(..), _
    | ListSchema(..), _
    | FixedListSchema(..), _
    | DictionarySchema(..), _
    | RecordSchema(..), _
    -> Error(ValueMistyped(path:, want: expected(schema), read: value))
  }
}

/// Why decimal text is not a canonical whole number.
pub type DecimalRefusal {
  NonCanonicalDecimal(read: String)
}

/// Reads a canonical integer with the BEAM's exact integer representation.
pub fn parse_decimal(text: String) -> Result(Int, DecimalRefusal) {
  case int.parse(text) {
    Ok(integer) -> {
      case int.to_string(integer) == text {
        True -> Ok(integer)
        False -> Error(NonCanonicalDecimal(read: text))
      }
    }
    Error(Nil) -> Error(NonCanonicalDecimal(read: text))
  }
}

fn bounded(
  integer: Int,
  text text: String,
  least least: Int,
  most most: Int,
  encoding encoding: IntegerEncoding,
  path path: String,
) -> Result(PreparedValue, ValueRefusal) {
  case integer >= least && integer <= most {
    False -> Error(NumberOutside(path:, read: text, least:, most:))
    True -> {
      case encoding {
        DecimalString ->
          Ok(PreparedValue(
            native: dynamic.int(integer),
            wire: json.string(int.to_string(integer)),
          ))
        JsonNumber ->
          Ok(PreparedValue(
            native: dynamic.int(integer),
            wire: json.int(integer),
          ))
      }
    }
  }
}

fn read_items(
  items: List(source.Value),
  schema: Schema,
  records: Dict(String, Schema),
  path: String,
) -> Result(PreparedValue, ValueRefusal) {
  use values <- result.try(
    items
    |> list.index_map(fn(value, index) { #(value, index) })
    |> list.try_map(fn(item) {
      read_value(
        item.0,
        schema,
        records,
        path <> "[" <> int.to_string(item.1) <> "]",
      )
    }),
  )
  Ok(PreparedValue(
    native: dynamic.list(list.map(values, fn(value) { value.native })),
    wire: json.array(values, of: fn(value) { value.wire }),
  ))
}

fn prepared_object(fields: Dict(String, PreparedValue)) -> PreparedValue {
  let fields = dict.to_list(fields)
  PreparedValue(
    native: fields
      |> list.map(fn(field) { #(dynamic.string(field.0), field.1.native) })
      |> dynamic.properties,
    wire: fields
      |> list.map(fn(field) { #(field.0, field.1.wire) })
      |> json.object,
  )
}

fn read_record(
  written: List(#(String, source.Value)),
  fields: List(#(String, Schema)),
  records: Dict(String, Schema),
  path: String,
  held: Dict(String, PreparedValue),
) -> Result(PreparedValue, ValueRefusal) {
  case written {
    [] -> {
      use _ <- result.try(
        list.try_each(fields, fn(field) {
          case dict.get(held, field.0) {
            Ok(_) -> Ok(Nil)
            Error(Nil) -> Error(MemberMissing(path: path <> "." <> field.0))
          }
        }),
      )
      Ok(prepared_object(held))
    }
    [#(name, value), ..rest] -> {
      let location = path <> "." <> name
      use _ <- result.try(refuse_repeated(held, name, location))
      case list.key_find(fields, name) {
        Error(Nil) -> Error(MemberUnknown(path: location))
        Ok(schema) -> {
          use value <- result.try(read_value(value, schema, records, location))
          read_record(
            rest,
            fields,
            records,
            path,
            dict.insert(held, name, value),
          )
        }
      }
    }
  }
}

fn read_dictionary(
  written: List(#(String, source.Value)),
  keys: DictionaryKeys,
  schema: Schema,
  records: Dict(String, Schema),
  path: String,
  held: Dict(String, PreparedValue),
) -> Result(PreparedValue, ValueRefusal) {
  case written {
    [] -> Ok(prepared_object(held))
    [#(name, value), ..rest] -> {
      let location = path <> "[" <> json.to_string(json.string(name)) <> "]"
      use _ <- result.try(refuse_repeated(held, name, location))
      use _ <- result.try(read_key(name, keys, location))
      use value <- result.try(read_value(value, schema, records, location))
      read_dictionary(
        rest,
        keys,
        schema,
        records,
        path,
        dict.insert(held, name, value),
      )
    }
  }
}

fn read_key(
  name: String,
  keys: DictionaryKeys,
  path: String,
) -> Result(Nil, ValueRefusal) {
  case keys {
    AnyKey -> Ok(Nil)
    LiteralKeys(values:) -> {
      case list.contains(values, name) {
        True -> Ok(Nil)
        False -> Error(KeyOutside(path:, read: name, keys: values))
      }
    }
  }
}

fn refuse_repeated(
  held: Dict(String, PreparedValue),
  name: String,
  path: String,
) -> Result(Nil, ValueRefusal) {
  case dict.get(held, name) {
    Ok(_) -> Error(MemberRepeated(path:))
    Error(Nil) -> Ok(Nil)
  }
}

fn expected(schema: Schema) -> String {
  case schema {
    TextSchema | LiteralsSchema(..) | InstantSchema -> "a string"
    IntegerSchema(encoding: DecimalString, ..) ->
      "a whole number written as a string"
    IntegerSchema(encoding: JsonNumber, ..) -> "a whole number"
    FiniteSchema -> "a finite number"
    TruthSchema -> "true or false"
    ListSchema(..) | FixedListSchema(..) -> "an array"
    RecordSchema(..) | DictionarySchema(..) | ReferenceSchema(..) -> "an object"
    NullableSchema(inner:) -> expected(inner)
  }
}

/// The exact location and message the request refusal publishes.
pub fn describe(refusal: ValueRefusal) -> #(String, String) {
  case refusal {
    JsonUnreadable(cause: source.EmptyBody(..)) -> #(
      "body",
      "The body holds no JSON value.",
    )
    JsonUnreadable(cause: source.MultipleValues(..)) -> #(
      "body",
      "The body holds more than one JSON value.",
    )
    JsonUnreadable(cause: source.MalformedJson(reason:, ..)) -> #(
      "body",
      "body is not JSON: " <> reason <> ".",
    )
    JsonUnreadable(cause: source.DecoderMismatch(errors:, ..)) -> #(
      "body",
      "JSON decoder: " <> string.inspect(errors),
    )
    MemberMissing(path:) -> #(path, path <> " is missing from the body.")
    MemberRepeated(path:) -> #(path, path <> " is written more than once.")
    MemberUnknown(path:) -> #(
      path,
      path <> " is not a member the body accepts.",
    )
    ValueMistyped(path:, want:, read:) -> #(
      path,
      path
        <> " must hold "
        <> want
        <> ", and it holds "
        <> source.text(read)
        <> ".",
    )
    LiteralOutside(path:, read:, ..) -> #(
      path,
      path
        <> " holds \""
        <> read
        <> "\", which is not one of the values it accepts.",
    )
    KeyOutside(path:, read:, ..) -> #(
      path,
      path
        <> " is under the key \""
        <> read
        <> "\", which is not one of the keys it accepts.",
    )
    NumberNotWhole(path:, read:) -> #(
      path,
      path <> " holds " <> read <> ", which is not written as a whole number.",
    )
    NumberOutside(path:, read:, least:, most:) -> #(
      path,
      path
        <> " holds "
        <> read
        <> ", which is outside "
        <> int.to_string(least)
        <> " to "
        <> int.to_string(most)
        <> ".",
    )
    DecimalNotCanonical(path:, read:) -> #(
      path,
      path <> " holds \"" <> read <> "\", which " <> decimal_requirement(),
    )
    InstantUnreadable(path:, read:) -> #(
      path,
      path <> " holds \"" <> read <> "\", which is not an RFC 3339 time.",
    )
    ListLengthWrong(path:, length:, read:) -> #(
      path,
      path
        <> " must hold exactly "
        <> int.to_string(length)
        <> " items, and it holds "
        <> int.to_string(read)
        <> ".",
    )
    ReferenceMissing(path:, name:) -> #(
      path,
      "record \"" <> name <> "\" was not declared",
    )
  }
}

/// The canonical decimal grammar shared by URL and JSON integer readers.
pub fn decimal_requirement() -> String {
  "is not a whole number written as canonical decimal text: an optional -, then 0 or a digit from 1 to 9 followed by any digits, and never -0."
}
