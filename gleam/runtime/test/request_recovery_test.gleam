import exception
import gleam/dynamic
import gleam/erlang/process
import gleam/string
import gleeunit/should
import nocontract_runtime/request_deadline
import nocontract_runtime/request_recovery as recovery

type Reply {
  PreparedReply(status: Int)
}

pub fn native_error_keeps_its_class_reason_and_request_test() {
  prove_diagnostic(
    fn() { raise_error("error witness") },
    exception.Errored(dynamic.string("error witness")),
  )
}

pub fn native_throw_keeps_its_class_reason_and_request_test() {
  prove_diagnostic(
    fn() { throw_value("throw witness") },
    exception.Thrown(dynamic.string("throw witness")),
  )
}

pub fn native_exit_keeps_its_class_reason_and_request_test() {
  prove_diagnostic(
    fn() { exit_process("exit witness") },
    exception.Exited(dynamic.string("exit witness")),
  )
}

pub fn killed_deadline_worker_keeps_its_exit_reason_without_a_stack_test() {
  let assert Ok(deadline) = request_deadline.parse_deadline(1000)
  let assert request_deadline.RequestWorkerExited(stopped:) =
    request_deadline.answer_before_deadline(within: deadline, run: fn(_) {
      process.kill(process.self())
      PreparedReply(status: 200)
    })
  let diagnostics = process.new_subject()
  let prepared = PreparedReply(status: 500)
  recovery.answer_worker_exit(
    evidence(),
    stopped:,
    prepare: fn() { prepared },
    record: process.send(diagnostics, _),
  )
  |> should.equal(prepared)
  let assert Ok(diagnostic) = process.receive(diagnostics, within: 0)
  diagnostic
  |> should.equal(recovery.CrashDiagnostic(
    request: evidence(),
    cause: recovery.WorkerStopped(reason: process.Killed),
    stack: recovery.StackNotAvailable,
  ))
}

fn evidence() -> recovery.RequestEvidence {
  recovery.RequestEvidence(
    method: "POST",
    path: "/orders/7",
    route: recovery.RouteMatched(method: "POST", path: "/orders/{order_id}"),
  )
}

fn prove_diagnostic(dispatch: fn() -> Reply, expected: exception.Exception) {
  let diagnostics = process.new_subject()
  let prepared = PreparedReply(status: 500)
  recovery.answer_request(
    evidence(),
    prepare: fn() { prepared },
    record: process.send(diagnostics, _),
    dispatch:,
  )
  |> should.equal(prepared)
  let assert Ok(diagnostic) = process.receive(diagnostics, within: 0)
  diagnostic.request |> should.equal(evidence())
  diagnostic.cause
  |> should.equal(recovery.NativeException(exception: expected))
  let assert recovery.CrashStack(text: stack) = diagnostic.stack
  stack |> string.contains("request_recovery_test") |> should.be_true
}

@external(erlang, "erlang", "error")
fn raise_error(reason: String) -> a

@external(erlang, "erlang", "throw")
fn throw_value(reason: String) -> a

@external(erlang, "erlang", "exit")
fn exit_process(reason: String) -> a
