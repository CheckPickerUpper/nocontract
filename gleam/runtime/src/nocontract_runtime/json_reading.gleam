import gleam/dynamic
import gleam/dynamic/decode
import gleam/json
import gleam/list
import gleam/result
import gleam/string

/// A JSON value retaining object order, duplicate members and number source text.
pub type Value {
  ObjectValue(fields: List(#(String, Value)))
  ArrayValue(items: List(Value))
  TextValue(text: String)
  NumberValue(text: String)
  BooleanValue(value: Bool)
  NullValue
}

/// Why the platform JSON decoder could not read one complete value.
pub type JsonRefusal {
  EmptyBody(content: String)
  MultipleValues(content: String, remaining: String)
  MalformedJson(content: String, reason: String)
  DecoderMismatch(content: String, errors: List(decode.DecodeError))
}

@external(erlang, "nocontract_json_ffi", "read_json")
fn platform_read(content: String) -> dynamic.Dynamic

fn value_decoder() -> decode.Decoder(Value) {
  use kind <- decode.field("kind", decode.string)
  case kind {
    "object" -> {
      use fields <- decode.field(
        "fields",
        decode.list({
          use key <- decode.subfield([0], decode.string)
          use value <- decode.subfield([1], decode.recursive(value_decoder))
          decode.success(#(key, value))
        }),
      )
      decode.success(ObjectValue(fields:))
    }
    "array" -> {
      use items <- decode.field(
        "items",
        decode.list(decode.recursive(value_decoder)),
      )
      decode.success(ArrayValue(items:))
    }
    "text" -> {
      use text <- decode.field("text", decode.string)
      decode.success(TextValue(text:))
    }
    "number" -> {
      use text <- decode.field("text", decode.string)
      decode.success(NumberValue(text:))
    }
    "boolean" -> {
      use value <- decode.field("value", decode.bool)
      decode.success(BooleanValue(value:))
    }
    "null" -> decode.success(NullValue)
    _ -> decode.failure(NullValue, "a platform JSON value")
  }
}

fn reading_decoder(
  content: String,
) -> decode.Decoder(Result(Value, JsonRefusal)) {
  use kind <- decode.field("kind", decode.string)
  case kind {
    "read" -> {
      use value <- decode.field("value", value_decoder())
      decode.success(Ok(value))
    }
    "empty" -> decode.success(Error(EmptyBody(content:)))
    "multiple" -> {
      use remaining <- decode.field("remaining", decode.string)
      decode.success(Error(MultipleValues(content:, remaining:)))
    }
    "malformed" -> {
      use reason <- decode.field("reason", decode.string)
      decode.success(Error(MalformedJson(content:, reason:)))
    }
    _ -> decode.failure(Error(EmptyBody(content:)), "a platform JSON result")
  }
}

/// Reads one value with OTP's JSON decoder and checks the foreign representation.
pub fn read(content: String) -> Result(Value, JsonRefusal) {
  content
  |> platform_read
  |> decode.run(reading_decoder(content))
  |> result.map_error(DecoderMismatch(content:, errors: _))
  |> result.flatten
}

/// The JSON spelling used in refusal evidence.
pub fn text(value: Value) -> String {
  case value {
    ObjectValue(fields:) -> {
      let members =
        list.map(fields, fn(field) {
          let #(key, value) = field
          json.to_string(json.string(key)) <> ":" <> text(value)
        })
      "{" <> string.join(members, ",") <> "}"
    }
    ArrayValue(items:) -> "[" <> string.join(list.map(items, text), ",") <> "]"
    TextValue(text:) -> json.to_string(json.string(text))
    NumberValue(text:) -> text
    BooleanValue(value: True) -> "true"
    BooleanValue(value: False) -> "false"
    NullValue -> "null"
  }
}
