package requestcrash

import "base:runtime"
import "core:c"
import "core:encoding/json"
import "core:fmt"
import "core:sys/posix"
import "../requestlifetime"

// Standalone_Dispatch protects a dispatch with no shared request completion.
Standalone_Dispatch :: struct #all_or_none {}

// Lifetime_Dispatch borrows the same completion cell as the request's other terminal paths.
Lifetime_Dispatch :: struct #all_or_none {
	lifetime: ^requestlifetime.Request_Lifetime,
}

// Request_Protection_Mode identifies whether a fatal dispatch must claim shared completion.
Request_Protection_Mode :: union #no_nil {
	Standalone_Dispatch,
	Lifetime_Dispatch,
}

// Request_Protection borrows prepared response and crash-log bytes until dispatch returns.
// The diagnostic prefix is a JSON object opened through its request fields and a trailing comma;
// the handler completes it with the cause and stack fields.
Request_Protection :: struct #all_or_none {
	socket:            posix.FD,
	response:          string,
	diagnostic_prefix: string,
	mode:              Request_Protection_Mode,
}

// Request_Answered holds the answer returned by a protected dispatch.
Request_Answered :: struct($Answer: typeid) #all_or_none {
	answer: Answer,
}

// Alternate_Stack_Not_Installed preserves the failed thread setup and requested stack size.
Alternate_Stack_Not_Installed :: struct #all_or_none {
	cause: posix.Errno,
	bytes: int,
}

// Request_Dispatch preserves a completed dispatch or the thread setup that prevented it.
Request_Dispatch :: union($Answer: typeid) #no_nil {
	Request_Answered(Answer),
	Alternate_Stack_Not_Installed,
}

@(private, not_an_error)
No_Request :: struct #all_or_none {}

@(private)
Current_Request :: union #no_nil {
	No_Request,
	Request_Protection,
}

@(private, not_an_error)
Unprepared_Thread :: struct #all_or_none {}

@(private)
Prepared_Thread :: struct #all_or_none {
	request: Current_Request,
}

@(private)
Thread_Protection :: union #no_nil {
	Unprepared_Thread,
	Prepared_Thread,
}

@(private, thread_local)
thread_protection: Thread_Protection

// Thread-local storage lives until the kernel has finished the thread, so the signal stack
// needs no allocation and no cleanup when a per-request deadline thread exits.
@(private, thread_local)
alternate_stack: [posix.SIGSTKSZ]u8

@(private)
Thread_Preparation :: union #no_nil {
	Prepared_Thread,
	Alternate_Stack_Not_Installed,
}

// answer_request protects the calling thread's dispatch and restores its enclosing request afterwards.
@(require_results)
answer_request :: proc(
	protection: Request_Protection,
	input: $Input,
	answer: proc(input: Input) -> $Answer,
) -> Request_Dispatch(Answer) {
	switch prepared in prepare_thread() {
	case Alternate_Stack_Not_Installed:
		return prepared
	case Prepared_Thread:
		thread_protection = Prepared_Thread{request = protection}
		defer thread_protection = prepared
		previous_assertion := context.assertion_failure_proc
		context.assertion_failure_proc = request_assertion_failed
		defer context.assertion_failure_proc = previous_assertion
		enclosing_assertion := recorded_assertion
		recorded_assertion = No_Assertion{}
		defer recorded_assertion = enclosing_assertion
		return Request_Answered(Answer){answer = answer(input)}
	}
	unreachable()
}

@(private, not_an_error)
No_Assertion :: struct #all_or_none {}

// Assertion_Recorded holds the JSON cause and stack fields an assertion encoded before it trapped.
@(private)
Assertion_Recorded :: struct #all_or_none {
	fields: string,
}

@(private)
Recorded_Assertion :: union #no_nil {
	No_Assertion,
	Assertion_Recorded,
}

@(private, thread_local)
recorded_assertion: Recorded_Assertion

@(private, require_results)
prepare_thread :: proc() -> Thread_Preparation {
	switch prepared in thread_protection {
	case Prepared_Thread:
		return prepared
	case Unprepared_Thread:
		alternate := posix.stack_t{ss_sp = raw_data(alternate_stack[:]), ss_flags = {}, ss_size = c.size_t(len(alternate_stack))}
		switch posix.sigaltstack(&alternate, nil) {
		case .OK:
			ready := Prepared_Thread{request = No_Request{}}
			thread_protection = ready
			return ready
		case .FAIL:
			return Alternate_Stack_Not_Installed{cause = posix.errno(), bytes = len(alternate_stack)}
		}
	}
	unreachable()
}

@(private)
Assertion_Fields :: struct #all_or_none {
	cause: string,
	stack: string,
}

// request_assertion_failed records the assertion's message and source location for the crash
// log, since the signal handler that writes the log sees only the trap's signal.
@(private)
request_assertion_failed :: proc(prefix, message: string, location: runtime.Source_Code_Location) -> ! {
	cause := prefix
	if len(message) > 0 {
		cause = fmt.tprintf("%s: %s", prefix, message)
	}
	stack := fmt.tprintf("%s(%d:%d)", location.file_path, location.line, location.column)
	encoded, failure := json.marshal(Assertion_Fields{cause = cause, stack = stack}, {spec = .JSON}, context.temp_allocator)
	// Without the encoded fields the trap's signal names the cause, as for any other crash.
	if failure == nil {
		// The object's braces are dropped so the fields close the prepared diagnostic.
		recorded_assertion = Assertion_Recorded{fields = string(encoded[1:len(encoded) - 1])}
	}
	runtime.default_assertion_contextless_failure_proc(prefix, message, location)
}
