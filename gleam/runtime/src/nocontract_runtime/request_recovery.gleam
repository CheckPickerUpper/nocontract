import exception
import gleam/erlang/process
import gleam/json
import gleam/string
import wisp

/// The declared route selected for a request, or a request outside the route table.
pub type VisitedRoute {
  RouteMatched(method: String, path: String)
  NoRouteMatched
}

/// The request and declared route whose dispatch is protected.
pub type RequestEvidence {
  RequestEvidence(method: String, path: String, route: VisitedRoute)
}

/// What ended a protected dispatch: an exception its own process raised, or the exit of the
/// deadline worker that ran it.
pub type CrashCause {
  NativeException(exception: exception.Exception)
  WorkerStopped(reason: process.ExitReason)
}

/// The crashed process's stack, or a worker that exited before its stack could be read.
pub type CrashStack {
  CrashStack(text: String)
  StackNotAvailable
}

/// A crashed dispatch with its cause, its stack and request evidence.
pub type CrashDiagnostic {
  CrashDiagnostic(
    request: RequestEvidence,
    cause: CrashCause,
    stack: CrashStack,
  )
}

type NativeCrash {
  NativeCrash(cause: exception.Exception, stack: String)
}

// The foreign function constructs this module's NativeCrash from Erlang's catch triple.
@external(erlang, "nocontract_request_recovery_ffi", "rescue")
fn rescue(dispatch: fn() -> answer) -> Result(answer, NativeCrash)

/// Runs typed dispatch before the response is written and records its native exception.
pub fn answer_request(
  request: RequestEvidence,
  prepare prepare: fn() -> answer,
  record record: fn(CrashDiagnostic) -> Nil,
  dispatch dispatch: fn() -> answer,
) -> answer {
  case rescue(dispatch) {
    Ok(answer) -> answer
    Error(NativeCrash(cause:, stack:)) -> {
      record(CrashDiagnostic(
        request:,
        cause: NativeException(exception: cause),
        stack: CrashStack(text: stack),
      ))
      prepare()
    }
  }
}

/// Records a deadline worker that exited without answering, from its monitor's Down message.
pub fn answer_worker_exit(
  request: RequestEvidence,
  stopped stopped: process.Down,
  prepare prepare: fn() -> answer,
  record record: fn(CrashDiagnostic) -> Nil,
) -> answer {
  let reason = case stopped {
    process.ProcessDown(reason:, ..) | process.PortDown(reason:, ..) -> reason
  }
  record(CrashDiagnostic(
    request:,
    cause: WorkerStopped(reason:),
    stack: StackNotAvailable,
  ))
  prepare()
}

/// Records the native reason and original stack through Wisp's application logger.
pub fn log_crash(diagnostic: CrashDiagnostic) -> Nil {
  let cause = case diagnostic.cause {
    NativeException(exception:) -> string.inspect(exception)
    WorkerStopped(reason:) -> string.inspect(reason)
  }
  let stack = case diagnostic.stack {
    CrashStack(text:) -> json.string(text)
    StackNotAvailable -> json.null()
  }
  json.object([
    #("method", json.string(diagnostic.request.method)),
    #("path", json.string(diagnostic.request.path)),
    #("route", json.string(route_path(diagnostic.request))),
    #("cause", json.string(cause)),
    #("stack", stack),
  ])
  |> json.to_string
  |> wisp.log_error
}

/// The matched path template, or the requested path when no route matched.
pub fn route_path(request: RequestEvidence) -> String {
  case request.route {
    RouteMatched(path:, ..) -> path
    NoRouteMatched -> request.path
  }
}
