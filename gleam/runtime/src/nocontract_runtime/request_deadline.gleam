import gleam/erlang/process

/// A positive, bounded duration in whole milliseconds.
pub opaque type RequestDeadline {
  RequestDeadline(milliseconds: Int)
}

/// Why a whole millisecond duration cannot configure a deadline.
pub type DeadlineRefusal {
  /// ergleam:code NC-1171
  RequestDeadlineOutside(timeout_milliseconds: Int, least: Int, most: Int)
}

/// Parses the whole milliseconds accepted by every NoContract server.
pub fn parse_deadline(
  milliseconds: Int,
) -> Result(RequestDeadline, DeadlineRefusal) {
  case milliseconds >= 1 && milliseconds <= 4_294_967_295 {
    True -> Ok(RequestDeadline(milliseconds:))
    False ->
      Error(RequestDeadlineOutside(
        timeout_milliseconds: milliseconds,
        least: 1,
        most: 4_294_967_295,
      ))
  }
}

/// A request's immutable cancellation and deadline evidence.
pub opaque type RequestLifetime {
  Unbounded
  DueAt(deadline_milliseconds: Int, timeout_milliseconds: Int)
}

/// Whether the listener has reached this request's cancellation deadline.
pub type RequestCancellation {
  RequestRunning
  DeadlineElapsed(timeout_milliseconds: Int)
}

@external(erlang, "nocontract_request_deadline_ffi", "monotonic_milliseconds")
fn monotonic_milliseconds() -> Int

/// Reads the deadline signal available to a handler and its child work.
pub fn cancellation(lifetime: RequestLifetime) -> RequestCancellation {
  case lifetime {
    Unbounded -> RequestRunning
    DueAt(deadline_milliseconds:, timeout_milliseconds:) -> {
      case monotonic_milliseconds() >= deadline_milliseconds {
        True -> DeadlineElapsed(timeout_milliseconds:)
        False -> RequestRunning
      }
    }
  }
}

/// Runs a checked handler under the caller's unbounded request lifetime.
pub fn without_deadline(run compute: fn(RequestLifetime) -> answer) -> answer {
  compute(Unbounded)
}

@external(erlang, "nocontract_request_deadline_ffi", "raise_exit")
fn raise_exit(reason: process.ExitReason) -> answer

/// Preserves an exited worker's cause instead of treating a crash as expiration.
pub fn worker_exited(stopped: process.Down) -> answer {
  case stopped {
    process.ProcessDown(reason:, ..) | process.PortDown(reason:, ..) ->
      raise_exit(reason)
  }
}

/// A request's computed answer, expiration, or worker exit.
pub type RequestCompletion(answer) {
  RequestCompleted(answer: answer)
  RequestExpired(
    timeout_milliseconds: Int,
    worker: process.Pid,
    monitor: process.Monitor,
  )
  RequestWorkerExited(stopped: process.Down)
}

type WorkerReply(answer) {
  AnswerReady(answer: answer)
  WorkerExited(stopped: process.Down)
}

@external(erlang, "nocontract_request_deadline_ffi", "with_monitored_worker")
fn with_monitored_worker(
  run: fn() -> answer,
  reply: process.Subject(answer),
  awaiting: fn(process.Pid, process.Monitor) -> completion,
) -> completion

/// Computes an answer in an isolated, monitored OTP worker. The reply alias and
/// monitor are deactivated and queued replies removed before cancellation is
/// signalled, so expiration never waits for application code to stop.
pub fn answer_before_deadline(
  within deadline: RequestDeadline,
  run compute: fn(RequestLifetime) -> answer,
) -> RequestCompletion(answer) {
  let timeout_milliseconds = deadline.milliseconds
  let deadline_milliseconds = monotonic_milliseconds() + timeout_milliseconds
  let lifetime = DueAt(deadline_milliseconds:, timeout_milliseconds:)
  let reply = process.new_subject()
  use worker, monitor <- with_monitored_worker(
    fn() { compute(lifetime) },
    reply,
  )
  let selector =
    process.new_selector()
    |> process.select_map(reply, AnswerReady(answer: _))
    |> process.select_specific_monitor(monitor, WorkerExited(stopped: _))
  case deadline_milliseconds - monotonic_milliseconds() {
    remaining if remaining > 0 -> {
      case process.selector_receive(selector, within: remaining) {
        Ok(AnswerReady(answer:)) -> {
          case cancellation(lifetime) {
            RequestRunning -> RequestCompleted(answer:)
            DeadlineElapsed(timeout_milliseconds:) ->
              RequestExpired(timeout_milliseconds:, worker:, monitor:)
          }
        }
        Ok(WorkerExited(stopped:)) -> RequestWorkerExited(stopped:)
        Error(Nil) -> RequestExpired(timeout_milliseconds:, worker:, monitor:)
      }
    }
    _ -> RequestExpired(timeout_milliseconds:, worker:, monitor:)
  }
}
