package balance

import "core:sync"
import "core:time"
import "core:log"

import "nocontract:odin/runtime/requestlifetime"

// DelayedReadInput names the shopper, currency and the fixture's preparation delay.
DelayedReadInput :: struct #all_or_none {
	shopper_id: i64,
	currency: Currency,
	delay_milliseconds: u16,
}

// ReadDelayedBalance lets preparation finish after cancellation to exercise a late answer.
//nocontract:route GET /shoppers/{shopper_id}/delayed-balance
@(require_results)
ReadDelayedBalance :: proc(input: DelayedReadInput, lifetime: requestlifetime.Handler_Lifetime) -> ReadOutcome {
	preparation := sync.Sema{}
	_ = sync.sema_wait_with_timeout(&preparation, time.Duration(input.delay_milliseconds)*time.Millisecond)
	switch scope in lifetime {
	case requestlifetime.No_Request_Deadline:
	case requestlifetime.Deadline_Lifetime:
		snapshot := requestlifetime.read_request_lifetime(scope.value)
		switch snapshot.completion {
		case .Deadline_Claimed:
			log.infof("The delayed balance observed cancellation after %d milliseconds", i64(snapshot.duration/time.Millisecond))
		case .Pending, .Answer_Claimed, .Crash_Claimed:
		}
	}
	return ReadBalance(ReadInput{shopper_id = input.shopper_id, currency = input.currency})
}
