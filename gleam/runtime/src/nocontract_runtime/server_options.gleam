import gleam/option
import nocontract_runtime/bind_address
import nocontract_runtime/request_deadline
import nocontract_runtime/request_layers

/// A listener configuration accepted only by parse and the typed option setters.
pub opaque type StartOptions {
  StartOptions(configuration: Configuration)
}

/// A read-only projection of the options consumed by the generated listener.
pub type Configuration {
  Configuration(
    address: bind_address.BindAddress,
    port: Int,
    layers: List(request_layers.Layer),
    deadline: option.Option(request_deadline.RequestDeadline),
  )
}

/// Why an address or port cannot start a listener.
pub type OptionsRefusal {
  EmptyAddress(address: String)
  AddressRefused(cause: bind_address.AddressRefusal)
  PortOutside(port: Int, least: Int, most: Int)
}

/// Checks the listener's address and port. Port zero asks the kernel to assign one.
pub fn parse(
  address address: String,
  port port: Int,
) -> Result(StartOptions, OptionsRefusal) {
  case address, port {
    "", _ -> Error(EmptyAddress(address:))
    _, port if port < 0 || port > 65_535 ->
      Error(PortOutside(port:, least: 0, most: 65_535))
    _, _ -> {
      case bind_address.parse(address) {
        Ok(address) ->
          Ok(
            StartOptions(configuration: Configuration(
              address:,
              port:,
              layers: [],
              deadline: option.None,
            )),
          )
        Error(cause) -> Error(AddressRefused(cause:))
      }
    }
  }
}

/// Selects ordered request and answer observers without exposing the HTTP response.
pub fn with_layers(
  options options: StartOptions,
  layers layers: List(request_layers.Layer),
) -> StartOptions {
  StartOptions(configuration: Configuration(..options.configuration, layers:))
}

/// Chooses a positive, bounded request deadline in whole milliseconds.
pub fn with_request_deadline(
  options options: StartOptions,
  milliseconds milliseconds: Int,
) -> Result(StartOptions, request_deadline.DeadlineRefusal) {
  case request_deadline.parse_deadline(milliseconds) {
    Ok(deadline) ->
      Ok(StartOptions(
        configuration: Configuration(
          ..options.configuration,
          deadline: option.Some(deadline),
        ),
      ))
    Error(cause) -> Error(cause)
  }
}

/// Reads checked settings without allowing their projection to start a listener.
pub fn read_configuration(options: StartOptions) -> Configuration {
  options.configuration
}
