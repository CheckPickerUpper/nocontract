import gleam/erlang/process
import gleam/function
import gleeunit/should
import nocontract_runtime/request_deadline

pub fn a_completed_request_keeps_its_answer_and_removes_its_monitor_test() {
  let assert Ok(deadline) = request_deadline.parse_deadline(1000)
  request_deadline.answer_before_deadline(within: deadline, run: fn(lifetime) {
    request_deadline.cancellation(lifetime)
    |> should.equal(request_deadline.RequestRunning)
    42
  })
  |> should.equal(request_deadline.RequestCompleted(answer: 42))
  assert_empty_mailbox()
}

pub fn an_expired_request_cancels_its_worker_and_removes_its_messages_test() {
  let assert Ok(deadline) = request_deadline.parse_deadline(1)
  let lifetimes = process.new_subject()
  let completion: request_deadline.RequestCompletion(Int) =
    request_deadline.answer_before_deadline(within: deadline, run: fn(lifetime) {
      process.send(lifetimes, lifetime)
      process.receive_forever(process.new_subject())
    })
  let assert request_deadline.RequestExpired(
    timeout_milliseconds: 1,
    worker:,
    ..,
  ) = completion
  let monitor = process.monitor(worker)
  let assert Ok(process.ProcessDown(pid:, ..)) =
    process.new_selector()
    |> process.select_specific_monitor(monitor, function.identity)
    |> process.selector_receive(within: 1000)
  pid |> should.equal(worker)
  process.is_alive(worker) |> should.be_false()
  lifetimes
  |> process.receive_forever
  |> request_deadline.cancellation
  |> should.equal(request_deadline.DeadlineElapsed(timeout_milliseconds: 1))
  assert_empty_mailbox()
}

fn assert_empty_mailbox() -> Nil {
  process.new_selector()
  |> process.select_other(function.identity)
  |> process.selector_receive(within: 0)
  |> should.equal(Error(Nil))
}

pub fn an_exited_worker_is_distinct_from_a_deadline_test() {
  let assert Ok(deadline) = request_deadline.parse_deadline(1000)
  let completion =
    request_deadline.answer_before_deadline(
      within: deadline,
      run: fn(_lifetime) {
        process.kill(process.self())
        42
      },
    )
  let assert request_deadline.RequestWorkerExited(stopped: process.ProcessDown(
    reason: process.Killed,
    ..,
  )) = completion
  assert_empty_mailbox()
}
