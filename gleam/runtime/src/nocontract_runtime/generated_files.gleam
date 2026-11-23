import gleam/bit_array
import gleam/dynamic
import gleam/dynamic/decode
import gleam/erlang/atom
import gleam/int
import gleam/string

/// Both byte streams captured by OTP's stderr_to_stdout port setting.
pub type CombinedOutput {
  CombinedOutput(bytes: BitArray)
}

/// The exact executable, argument vector and working directory checked.
pub type CheckCommand {
  CheckCommand(directory: String, executable: String, arguments: List(String))
}

/// An exited Go tool process and every byte it wrote.
pub type CheckExecution {
  CheckExecution(
    command: CheckCommand,
    status: Int,
    output: CombinedOutput,
    elapsed_milliseconds: Int,
  )
}

/// Why the project's generated files could not be confirmed current.
pub type CheckRefusal {
  DirectoryUnreadable(
    arguments: List(String),
    cause: String,
    elapsed_milliseconds: Int,
  )
  GoNotFound(
    directory: String,
    arguments: List(String),
    elapsed_milliseconds: Int,
  )
  CommandNotStarted(
    command: CheckCommand,
    cause: String,
    elapsed_milliseconds: Int,
  )
  CommandInterrupted(
    command: CheckCommand,
    cause: String,
    output: CombinedOutput,
    elapsed_milliseconds: Int,
  )
  CommandExited(execution: CheckExecution)
  NativeResultMalformed(
    arguments: List(String),
    returned: String,
    errors: List(decode.DecodeError),
  )
}

@external(erlang, "nocontract_generated_files_ffi", "check")
fn platform_check(arguments: List(String)) -> dynamic.Dynamic

fn process_decoder(
  command: CheckCommand,
  elapsed_milliseconds: Int,
) -> decode.Decoder(Result(CheckExecution, CheckRefusal)) {
  use kind <- decode.subfield([0], atom.decoder())
  case atom.to_string(kind) {
    "native_exited" -> {
      use status <- decode.subfield([1], decode.int)
      use bytes <- decode.subfield([2], decode.bit_array)
      CheckExecution(
        command:,
        status:,
        output: CombinedOutput(bytes:),
        elapsed_milliseconds:,
      )
      |> exit_result
      |> decode.success
    }
    "native_interrupted" -> {
      use cause <- decode.subfield([1], decode.dynamic)
      use bytes <- decode.subfield([2], decode.bit_array)
      decode.success(
        Error(CommandInterrupted(
          command:,
          cause: string.inspect(cause),
          output: CombinedOutput(bytes:),
          elapsed_milliseconds:,
        )),
      )
    }
    _ ->
      decode.failure(
        Error(
          NativeResultMalformed(
            arguments: command.arguments,
            returned: atom.to_string(kind),
            errors: [],
          ),
        ),
        "a port result",
      )
  }
}

fn check_decoder(
  arguments: List(String),
) -> decode.Decoder(Result(CheckExecution, CheckRefusal)) {
  use kind <- decode.subfield([0], atom.decoder())
  case atom.to_string(kind) {
    "native_directory_unreadable" -> {
      use cause <- decode.subfield([1], decode.dynamic)
      use elapsed_milliseconds <- decode.subfield([2], decode.int)
      decode.success(
        Error(DirectoryUnreadable(
          arguments:,
          cause: string.inspect(cause),
          elapsed_milliseconds:,
        )),
      )
    }
    "native_go_not_found" -> {
      use directory <- decode.subfield([1], decode.string)
      use elapsed_milliseconds <- decode.subfield([2], decode.int)
      decode.success(
        Error(GoNotFound(directory:, arguments:, elapsed_milliseconds:)),
      )
    }
    "native_not_started" -> {
      use directory <- decode.subfield([1], decode.string)
      use executable <- decode.subfield([2], decode.string)
      use cause <- decode.subfield([3], decode.dynamic)
      use elapsed_milliseconds <- decode.subfield([4], decode.int)
      decode.success(
        Error(CommandNotStarted(
          command: CheckCommand(directory:, executable:, arguments:),
          cause: string.inspect(cause),
          elapsed_milliseconds:,
        )),
      )
    }
    "native_completed" -> {
      use directory <- decode.subfield([1], decode.string)
      use executable <- decode.subfield([2], decode.string)
      use elapsed_milliseconds <- decode.subfield([4], decode.int)
      decode.at(
        [3],
        process_decoder(
          CheckCommand(directory:, executable:, arguments:),
          elapsed_milliseconds,
        ),
      )
    }
    _ ->
      decode.failure(
        Error(
          NativeResultMalformed(
            arguments:,
            returned: atom.to_string(kind),
            errors: [],
          ),
        ),
        "a NoContract check result",
      )
  }
}

/// Runs the application's declared Go tool dependency from the test command's directory.
pub fn check(project: String) -> Result(CheckExecution, CheckRefusal) {
  let arguments = ["tool", "nocontract", "check", "-project", project]
  let returned = platform_check(arguments)
  case decode.run(returned, check_decoder(arguments)) {
    Ok(execution) -> execution
    Error(errors) ->
      Error(NativeResultMalformed(
        arguments:,
        returned: string.inspect(returned),
        errors:,
      ))
  }
}

fn exit_result(
  execution: CheckExecution,
) -> Result(CheckExecution, CheckRefusal) {
  case execution.status {
    0 -> Ok(execution)
    _ -> Error(CommandExited(execution:))
  }
}

fn describe_output(output: CombinedOutput) -> String {
  case bit_array.to_string(output.bytes) {
    Ok(text) -> text
    Error(Nil) -> string.inspect(output.bytes)
  }
}

/// Formats the preserved command evidence and original NoContract diagnostics for a test failure.
pub fn describe(refusal: CheckRefusal) -> String {
  case refusal {
    CommandExited(execution:) ->
      "NoContract check exited "
      <> int.to_string(execution.status)
      <> " in "
      <> execution.command.directory
      <> ": "
      <> string.inspect(execution.command.arguments)
      <> "\n"
      <> describe_output(execution.output)
    CommandInterrupted(command:, cause:, output:, elapsed_milliseconds:) ->
      string.inspect(#(command, cause, elapsed_milliseconds))
      <> "\n"
      <> describe_output(output)
    DirectoryUnreadable(arguments:, cause:, elapsed_milliseconds:) ->
      string.inspect(#(arguments, cause, elapsed_milliseconds))
    GoNotFound(directory:, arguments:, elapsed_milliseconds:) ->
      "Go executable not found: "
      <> string.inspect(#(directory, arguments, elapsed_milliseconds))
    CommandNotStarted(command:, cause:, elapsed_milliseconds:) ->
      string.inspect(#(command, cause, elapsed_milliseconds))
    NativeResultMalformed(arguments:, returned:, errors:) ->
      "Malformed NoContract check result: "
      <> string.inspect(#(arguments, returned, errors))
  }
}
