package requestlifetime

import "core:sync"
import "core:time"

// No_Request_Deadline retains the caller's request lifetime without a server deadline.
No_Request_Deadline :: struct #all_or_none {}

// Deadline_Lifetime is the server-owned cancellation cell shared with a route's work.
Deadline_Lifetime :: struct #all_or_none {
	value: ^Request_Lifetime,
}

// Handler_Lifetime is deadline evidence borrowed until the route and its joined child work return.
Handler_Lifetime :: union #no_nil {
	No_Request_Deadline,
	Deadline_Lifetime,
}

// Completion_Kind is the one pending or terminal state shared by request workers.
Completion_Kind :: enum u32 {
	Pending,
	Answer_Claimed,
	Deadline_Claimed,
	Crash_Claimed,
}

// Request_Lifetime occupies its owner's stable storage until all handlers and
// native completion callbacks finish. Initialization precedes publication;
// subsequent completion reads and claims are atomic.
Request_Lifetime :: struct #all_or_none {
	deadline: time.Tick,
	duration: time.Duration,
	completion: Completion_Kind,
}

// Initialized identifies the owner's initialized request lifetime.
Initialized :: struct #all_or_none {
	lifetime: ^Request_Lifetime,
}

// Duration_Not_Positive retains the duration that left the storage untouched.
Duration_Not_Positive :: struct #all_or_none {
	duration: time.Duration,
}

// Initialization either initializes a lifetime or refuses its duration.
Initialization :: union #no_nil {
	Initialized,
	Duration_Not_Positive,
}

// Claimed identifies the attempt that selected the request's terminal state.
Claimed :: struct #all_or_none {}

// Already_Claimed retains the attempted kind after a terminal state was selected.
Already_Claimed :: struct #all_or_none {
	requested: Completion_Kind,
}

// Claim_Decision says whether this attempt selected the terminal state.
Claim_Decision :: union #no_nil {
	Claimed,
	Already_Claimed,
}

// Completion_Claim retains the winning kind and this attempt's decision.
Completion_Claim :: struct #all_or_none {
	winner: Completion_Kind,
	decision: Claim_Decision,
}

// Request_Snapshot is a transport-free view of the deadline and one atomic completion read.
Request_Snapshot :: struct #all_or_none {
	deadline: time.Tick,
	duration: time.Duration,
	completion: Completion_Kind,
}

// initialize_request_lifetime records the monotonic deadline before the owner publishes storage.
@(require_results)
initialize_request_lifetime :: proc "contextless" (
	storage: ^Request_Lifetime,
	started_at: time.Tick,
	duration: time.Duration,
) -> Initialization {
	if duration <= 0 {
		return Duration_Not_Positive{duration = duration}
	}
	storage^ = Request_Lifetime{
		deadline = time.tick_add(started_at, duration),
		duration = duration,
		completion = .Pending,
	}
	return Initialized{lifetime = storage}
}

// claim_answer attempts to select a computed answer without carrying a transport value.
@(require_results)
claim_answer :: proc "contextless" (lifetime: ^Request_Lifetime) -> Completion_Claim {
	return claim_request_completion(lifetime, .Answer_Claimed)
}

// claim_deadline signals cancellation when the owner's native deadline event fires.
@(require_results)
claim_deadline :: proc "contextless" (lifetime: ^Request_Lifetime) -> Completion_Claim {
	return claim_request_completion(lifetime, .Deadline_Claimed)
}

// claim_crash attempts to select the fatal path through the same lock-free completion cell.
@(require_results)
claim_crash :: proc "contextless" (lifetime: ^Request_Lifetime) -> Completion_Claim {
	return claim_request_completion(lifetime, .Crash_Claimed)
}

@(private)
@(require_results)
claim_request_completion :: proc "contextless" (
	lifetime: ^Request_Lifetime,
	$requested: Completion_Kind,
) -> Completion_Claim where requested != .Pending {
	previous := sync.atomic_compare_exchange_strong(&lifetime.completion, .Pending, requested)
	switch previous {
	case .Pending:
		return Completion_Claim{winner = requested, decision = Claimed{}}
	case .Answer_Claimed, .Deadline_Claimed, .Crash_Claimed:
		return Completion_Claim{winner = previous, decision = Already_Claimed{requested = requested}}
	}
	unreachable()
}

// read_request_lifetime copies the deadline facts and reads the selected completion atomically.
@(require_results)
read_request_lifetime :: proc "contextless" (lifetime: ^Request_Lifetime) -> Request_Snapshot {
	return Request_Snapshot{
		deadline = lifetime.deadline,
		duration = lifetime.duration,
		completion = sync.atomic_load(&lifetime.completion),
	}
}
