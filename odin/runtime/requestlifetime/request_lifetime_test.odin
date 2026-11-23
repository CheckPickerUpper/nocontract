#+test
package requestlifetime

import "core:sync"
import "core:testing"
import "core:thread"
import "core:time"

@(test)
nonpositive_durations_leave_the_storage_untouched :: proc(test: ^testing.T) {
	durations := []time.Duration{0, -time.Nanosecond}
	for duration in durations {
		started_at := time.tick_now()
		storage := Request_Lifetime{
			deadline = started_at,
			duration = time.Second,
			completion = .Crash_Claimed,
		}
		switch initialized in initialize_request_lifetime(&storage, started_at, duration) {
		case Initialized:
			testing.expect(test, false, "a nonpositive duration initialized a request")
		case Duration_Not_Positive:
			testing.expect_value(test, initialized.duration, duration)
		}
		snapshot := read_request_lifetime(&storage)
		testing.expect_value(test, snapshot.duration, time.Second)
		testing.expect_value(test, snapshot.completion, Completion_Kind.Crash_Claimed)
	}
}

@(test)
a_handler_observes_expiration_and_cannot_claim_a_late_answer :: proc(test: ^testing.T) {
	started_at := time.tick_now()
	storage := new_clone(Request_Lifetime{
		deadline = started_at,
		duration = 0,
		completion = .Pending,
	})
	defer free(storage)
	switch initialized in initialize_request_lifetime(storage, started_at, time.Nanosecond) {
	case Initialized:
		prove_handler_cancellation(test, initialized.lifetime, started_at)
	case Duration_Not_Positive:
		testing.expect(test, false, "a positive duration was refused")
	}
}

@(private)
Waiting :: struct #all_or_none {}

@(private)
Observed :: struct #all_or_none {
	snapshot: Request_Snapshot,
	claim: Completion_Claim,
}

@(private)
Handler_Observation :: union #no_nil {
	Waiting,
	Observed,
}

@(private)
Handler_Proof :: struct #all_or_none {
	lifetime: ^Request_Lifetime,
	ready: sync.Sema,
	resume: sync.Sema,
	observation: ^Handler_Observation,
}

@(private)
observe_expiration :: proc(proof: ^Handler_Proof) {
	sync.sema_post(&proof.ready)
	sync.sema_wait(&proof.resume)
	proof.observation^ = Observed{
		snapshot = read_request_lifetime(proof.lifetime),
		claim = claim_answer(proof.lifetime),
	}
}

@(private)
prove_handler_cancellation :: proc(test: ^testing.T, lifetime: ^Request_Lifetime, started_at: time.Tick) {
	observation := new_clone(Handler_Observation(Waiting{}))
	defer free(observation)
	proof := new_clone(Handler_Proof{
		lifetime = lifetime,
		ready = {},
		resume = {},
		observation = observation,
	})
	defer free(proof)
	worker := thread.create_and_start_with_poly_data(proof, observe_expiration)
	defer thread.destroy(worker)
	if !sync.sema_wait_with_timeout(&proof.ready, time.Second) {
		testing.expect(test, false, "the handler did not announce that it started")
		sync.sema_post(&proof.resume)
		thread.join(worker)
		return
	}
	snapshot := read_request_lifetime(lifetime)
	expiration := sync.Sema{}
	testing.expect(test, !sync.sema_wait_with_timeout(&expiration, time.tick_diff(time.tick_now(), snapshot.deadline)))
	claimed := claim_deadline(lifetime)
	switch _ in claimed.decision {
	case Claimed:
		testing.expect_value(test, claimed.winner, Completion_Kind.Deadline_Claimed)
	case Already_Claimed:
		testing.expect(test, false, "the pending request refused its deadline claim")
	}
	sync.sema_post(&proof.resume)
	thread.join(worker)
	switch observed in observation^ {
	case Waiting:
		testing.expect(test, false, "the handler ended without observing cancellation")
	case Observed:
		testing.expect_value(test, observed.snapshot.duration, time.Nanosecond)
		testing.expect_value(test, time.tick_diff(started_at, observed.snapshot.deadline), time.Nanosecond)
		testing.expect_value(test, observed.snapshot.completion, Completion_Kind.Deadline_Claimed)
		switch decision in observed.claim.decision {
		case Claimed:
			testing.expect(test, false, "the late answer replaced the deadline")
		case Already_Claimed:
			testing.expect_value(test, observed.claim.winner, Completion_Kind.Deadline_Claimed)
			testing.expect_value(test, decision.requested, Completion_Kind.Answer_Claimed)
		}
	}
}

@(private)
Completion_Case :: struct #all_or_none {
	claim: proc "contextless" (^Request_Lifetime) -> Completion_Claim,
	winner: Completion_Kind,
}

@(test)
answers_and_crashes_claim_the_same_cell_once :: proc(test: ^testing.T) {
	examples := []Completion_Case{
		Completion_Case{claim = claim_answer, winner = .Answer_Claimed},
		Completion_Case{claim = claim_crash, winner = .Crash_Claimed},
	}
	for example in examples {
		started_at := time.tick_now()
		storage := Request_Lifetime{
			deadline = started_at,
			duration = 0,
			completion = .Pending,
		}
		switch initialized in initialize_request_lifetime(&storage, started_at, time.Second) {
		case Initialized:
			claimed := example.claim(initialized.lifetime)
			switch _ in claimed.decision {
			case Claimed:
				testing.expect_value(test, claimed.winner, example.winner)
			case Already_Claimed:
				testing.expect(test, false, "the pending request refused its first claim")
			}
			late := claim_deadline(initialized.lifetime)
			switch decision in late.decision {
			case Claimed:
				testing.expect(test, false, "a deadline replaced the first claim")
			case Already_Claimed:
				testing.expect_value(test, late.winner, example.winner)
				testing.expect_value(test, decision.requested, Completion_Kind.Deadline_Claimed)
			}
		case Duration_Not_Positive:
			testing.expect(test, false, "a positive duration was refused")
		}
	}
}
