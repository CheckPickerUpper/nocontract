package requestcrash

import "core:c"
import "core:mem"
import "core:sys/posix"
import "../requestlifetime"

// Handlers_Installed identifies the fatal signals whose handlers were installed.
@(not_an_error)
Handlers_Installed :: struct #all_or_none {
	signals: []posix.Signal,
}

// Handler_Not_Installed preserves the signal, original system error and preceding installations.
Handler_Not_Installed :: struct #all_or_none {
	signal:    posix.Signal,
	cause:     posix.Errno,
	installed: []posix.Signal,
}

// Handler_Installation is the complete or partial result of installing fatal signal handlers.
Handler_Installation :: union #no_nil {
	Handlers_Installed,
	Handler_Not_Installed,
}

@(private)
Fatal_Handler :: struct #all_or_none {
	signal:  posix.Signal,
	handler: proc "c" (posix.Signal),
}

// One handler per signal carries that signal's fixed crash-log fields, so the handler never
// looks a name up for a signal it was not installed for.
@(private, rodata)
fatal_handlers := [?]Fatal_Handler{
	{signal = .SIGILL, handler = proc "c" (signal: posix.Signal) { request_signal_received(signal, `"cause":"SIGILL","stack":null`) }},
	{signal = .SIGTRAP, handler = proc "c" (signal: posix.Signal) { request_signal_received(signal, `"cause":"SIGTRAP","stack":null`) }},
	{signal = .SIGSEGV, handler = proc "c" (signal: posix.Signal) { request_signal_received(signal, `"cause":"SIGSEGV","stack":null`) }},
	{signal = .SIGBUS, handler = proc "c" (signal: posix.Signal) { request_signal_received(signal, `"cause":"SIGBUS","stack":null`) }},
	{signal = .SIGFPE, handler = proc "c" (signal: posix.Signal) { request_signal_received(signal, `"cause":"SIGFPE","stack":null`) }},
}

// install_handlers installs process-wide fatal handlers before the worker accepts requests.
@(require_results)
install_handlers :: proc(allocator: mem.Allocator) -> Handler_Installation {
	// A write to a closed peer would otherwise end the worker by SIGPIPE before the handler
	// re-raises the original signal; blocked, the write fails with EPIPE instead.
	mask := posix.sigset_t{}
	_ = posix.sigemptyset(&mask)
	_ = posix.sigaddset(&mask, .SIGPIPE)
	installed := make([dynamic]posix.Signal, allocator)
	for fatal in fatal_handlers {
		action := posix.sigaction_t{
			sa_handler = fatal.handler,
			sa_mask = mask,
			sa_flags = {.ONSTACK},
			sa_restorer = nil,
		}
		switch posix.sigaction(fatal.signal, &action, nil) {
		case .OK:
			append(&installed, fatal.signal)
		case .FAIL:
			return Handler_Not_Installed{signal = fatal.signal, cause = posix.errno(), installed = installed[:]}
		}
	}
	return Handlers_Installed{signals = installed[:]}
}

@(private)
request_signal_received :: proc "contextless" (signal: posix.Signal, signal_fields: string) {
	switch prepared in thread_protection {
	case Unprepared_Thread:
	case Prepared_Thread:
		switch request in prepared.request {
		case No_Request:
		case Request_Protection:
			fields := signal_fields
			switch recorded in recorded_assertion {
			case No_Assertion:
			case Assertion_Recorded:
				fields = recorded.fields
			}
			switch mode in request.mode {
			case Standalone_Dispatch:
				write_prepared_crash_answer(request, fields)
			case Lifetime_Dispatch:
				switch claimed in requestlifetime.claim_crash(mode.lifetime).decision {
				case requestlifetime.Claimed:
					write_prepared_crash_answer(request, fields)
				case requestlifetime.Already_Claimed:
				}
			}
		}
	}
	mask := posix.sigset_t{}
	_ = posix.sigemptyset(&mask)
	action := posix.sigaction_t{
		sa_handler = cast(proc "c" (posix.Signal))posix.SIG_DFL,
		sa_mask = mask,
		sa_flags = {},
		sa_restorer = nil,
	}
	_ = posix.sigaction(signal, &action, nil)
	_ = posix.sigaddset(&mask, signal)
	_ = posix.pthread_sigmask(.UNBLOCK, &mask, nil)
	_ = posix.raise(signal)
	posix._exit(128 + i32(signal))
}

@(private)
write_prepared_crash_answer :: proc "contextless" (request: Request_Protection, fields: string) {
	_ = posix.write(request.socket, raw_data(request.response), uint(len(request.response)))
	closing := "}\n"
	// One gathered write keeps the log line whole beside other threads' stderr writes.
	parts := [?]posix.iovec{
		{iov_base = raw_data(request.diagnostic_prefix), iov_len = c.size_t(len(request.diagnostic_prefix))},
		{iov_base = raw_data(fields), iov_len = c.size_t(len(fields))},
		{iov_base = raw_data(closing), iov_len = c.size_t(len(closing))},
	}
	_ = posix.writev(posix.STDERR_FILENO, raw_data(parts[:]), c.int(len(parts)))
}
