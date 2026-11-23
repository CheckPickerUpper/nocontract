import gleam/dict.{type Dict}
import gleam/option.{type Option, Some}

/// Reads a normalized credential header and returns the application's typed guard decision.
/// Missing or empty credentials call `missing`; nonempty credentials call `verify` once,
/// with their text unchanged. `None` admits the request and `Some` carries its refusal.
pub fn check(
  headers: Dict(String, String),
  header: String,
  missing: fn() -> refusal,
  verify: fn(String) -> Option(refusal),
) -> Option(refusal) {
  case dict.get(headers, header) {
    Ok("") | Error(Nil) -> Some(missing())
    Ok(credential) -> verify(credential)
  }
}
