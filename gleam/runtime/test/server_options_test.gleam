import gleam/erlang/process
import gleam/list
import gleam/option
import gleeunit/should
import nocontract_runtime/bind_address
import nocontract_runtime/request_deadline
import nocontract_runtime/request_layers
import nocontract_runtime/server_options

pub fn listener_options_start_without_layers_or_a_deadline_test() {
  let assert Ok(options) = server_options.parse(address: "localhost", port: 0)
  let configuration = server_options.read_configuration(options)
  bind_address.to_string(configuration.address) |> should.equal("localhost")
  configuration.port |> should.equal(0)
  list.length(configuration.layers) |> should.equal(0)
  configuration.deadline |> should.equal(option.None)
}

pub fn optional_settings_keep_the_address_port_and_each_others_last_value_test() {
  let messages = process.new_subject()
  let assert Ok(options) =
    server_options.parse(address: "127.0.0.1", port: 8080)
  let options =
    server_options.with_layers(options:, layers: [
      recording_layer(messages, "first"),
    ])
  let assert Ok(options) =
    server_options.with_request_deadline(options:, milliseconds: 10)
  let options =
    server_options.with_layers(options:, layers: [
      recording_layer(messages, "last"),
    ])
  let assert Ok(first_deadline) = request_deadline.parse_deadline(10)
  server_options.read_configuration(options).deadline
  |> should.equal(option.Some(first_deadline))

  let assert Ok(options) =
    server_options.with_request_deadline(options:, milliseconds: 20)
  let configuration = server_options.read_configuration(options)
  bind_address.to_string(configuration.address) |> should.equal("127.0.0.1")
  configuration.port |> should.equal(8080)
  let assert Ok(last_deadline) = request_deadline.parse_deadline(20)
  configuration.deadline |> should.equal(option.Some(last_deadline))
  list.length(configuration.layers) |> should.equal(1)
  let finish =
    request_layers.enter(
      configuration.layers,
      request_layers.Visit(method: "GET", path: "/", route: "/"),
    )
  finish(request_layers.Answered(status: 200, code: option.None))
  process.receive(messages, within: 0) |> should.equal(Ok("last"))
}

fn recording_layer(
  messages: process.Subject(String),
  name: String,
) -> request_layers.Layer {
  fn(_visit) { fn(_answered) { process.send(messages, name) } }
}
