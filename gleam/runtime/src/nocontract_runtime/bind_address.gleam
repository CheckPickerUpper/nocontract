import gleam/dynamic
import gleam/dynamic/decode
import gleam/erlang/atom
import gleam/erlang/charlist
import gleam/result

/// A localhost name or IP literal accepted by OTP's address parser.
pub opaque type BindAddress {
  BindAddress(text: String)
}

/// Why a string cannot be passed to Mist's address binding.
pub type AddressRefusal {
  InvalidLiteral(address: String, reason: String)
  AddressDecoderMismatch(address: String, errors: List(decode.DecodeError))
}

@external(erlang, "inet", "parse_address")
fn platform_parse(address: charlist.Charlist) -> dynamic.Dynamic

fn address_decoder(
  address: String,
) -> decode.Decoder(Result(BindAddress, AddressRefusal)) {
  use status <- decode.subfield([0], atom.decoder())
  case atom.to_string(status) {
    "ok" -> decode.success(Ok(BindAddress(text: address)))
    "error" -> {
      use reason <- decode.subfield([1], atom.decoder())
      decode.success(
        Error(InvalidLiteral(address:, reason: atom.to_string(reason))),
      )
    }
    status ->
      decode.failure(
        Error(InvalidLiteral(address:, reason: status)),
        expected: "inet:parse_address result",
      )
  }
}

/// Accepts localhost or parses an IP literal with OTP before the listener is built.
pub fn parse(address: String) -> Result(BindAddress, AddressRefusal) {
  case address {
    "localhost" -> Ok(BindAddress(text: address))
    _ ->
      address
      |> charlist.from_string
      |> platform_parse
      |> decode.run(address_decoder(address))
      |> result.map_error(AddressDecoderMismatch(address:, errors: _))
      |> result.flatten
  }
}

/// The validated string Mist accepts for binding.
pub fn to_string(address: BindAddress) -> String {
  address.text
}
