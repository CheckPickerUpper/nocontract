import gleam/dict
import gleeunit
import nocontract_runtime/value_reading as value

/// Runs the runtime's test entry point.
pub fn main() -> Nil {
  gleeunit.main()
}

pub fn a_small_integer_refusal_keeps_its_exponent_text_test() {
  let schema =
    value.RecordSchema(fields: [
      #("note", value.TextSchema),
      #(
        "nested",
        value.RecordSchema(fields: [
          #(
            "quantity",
            value.IntegerSchema(least: 0, most: 255, encoding: value.JsonNumber),
          ),
        ]),
      ),
    ])
  let assert Error(value.NumberNotWhole(
    path: "body.nested.quantity",
    read: "1e0",
  )) =
    value.read_body(
      "{\"note\":\"12 \\\"34\",\"nested\":{\"quantity\":1e0}}",
      schema,
      dict.new(),
    )
}
