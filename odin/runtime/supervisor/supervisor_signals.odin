package supervisor

import "core:sys/posix"
import "core:c"
import "core:os"
import "core:time"

foreign import signal_library "system:c"

foreign signal_library {
	@(private)
	sigtimedwait :: proc "c" (set: ^posix.sigset_t, report: ^posix.siginfo_t, timeout: ^posix.timespec) -> c.int ---
}

@(private, rodata)
control_signals := [?]posix.Signal{.SIGINT, .SIGTERM, .SIGCHLD, .SIGUSR1}

@(private, require_results)
control_mask :: proc() -> posix.sigset_t {
	mask := posix.sigset_t{}
	_ = posix.sigemptyset(&mask)
	for signal in control_signals {
		_ = posix.sigaddset(&mask, signal)
	}
	return mask
}

// Shutdown_Requested records a received shutdown, including one received after a worker died.
@(not_an_error)
Shutdown_Requested :: struct #all_or_none {
	signal: posix.Signal,
}

@(private)
Worker_Notification :: enum {
	Readiness,
	Child,
}

@(private, not_an_error)
Worker_Notified :: struct #all_or_none {
	kind: Worker_Notification,
}

@(private)
Control_Signal :: union #no_nil {
	Shutdown_Requested,
	Worker_Notified,
	Signal_Outside_Set,
}

@(private)
Control_Wait :: union #no_nil {
	Control_Signal,
	Worker_Shutdown_Expired,
	Supervision_Refusal,
}

@(private, require_results)
wait_control :: proc(mask: posix.sigset_t, worker: os.Process, intent: Shutdown_Intent) -> Control_Wait {
	waited_mask := mask
	switch stopping in intent {
	case Running:
		signal := posix.Signal.NONE
		cause := posix.sigwait(&waited_mask, &signal)
		if cause != .NONE {
			return Supervision_Refusal(Signal_Not_Received{mask = mask, cause = cause})
		}
		return Control_Wait(read_control_signal(signal, mask))
	case Stopping:
		expired := Worker_Shutdown_Expired{process = worker, signal = stopping.signal, period = stopping.period}
		// A past deadline still waits with a zero timeout, so a signal already pending when the
		// period ran out is read before the period is reported expired.
		remaining := max(time.tick_diff(time.tick_now(), stopping.deadline), 0)
		timeout := posix.timespec{
			tv_sec = posix.time_t(remaining / time.Second),
			tv_nsec = c.long(remaining % time.Second),
		}
		report := posix.siginfo_t{}
		signal := sigtimedwait(&waited_mask, &report, &timeout)
		if signal == -1 {
			cause := posix.errno()
			if cause == .EAGAIN {
				return expired
			}
			return Supervision_Refusal(Signal_Not_Received{mask = mask, cause = cause})
		}
		return Control_Wait(read_control_signal(posix.Signal(signal), mask))
	}
	unreachable()
}

@(private, require_results)
read_control_signal :: proc(signal: posix.Signal, mask: posix.sigset_t) -> Control_Signal {
	switch signal {
	case .SIGINT, .SIGTERM:
		return Shutdown_Requested{signal = signal}
	case .SIGUSR1:
		return Worker_Notified{kind = .Readiness}
	case .SIGCHLD:
		return Worker_Notified{kind = .Child}
	case .NONE, .SIGABRT, .SIGFPE, .SIGILL, .SIGSEGV, .SIGALRM,
	     .SIGBUS, .SIGCONT, .SIGHUP, .SIGKILL, .SIGPIPE, .SIGQUIT,
	     .SIGSTOP, .SIGTSTP, .SIGTTIN, .SIGTTOU, .SIGUSR2, .SIGPOLL,
	     .SIGPROF, .SIGSYS, .SIGTRAP, .SIGURG, .SIGVTALRM, .SIGXCPU, .SIGXFSZ:
		return Signal_Outside_Set{signal = signal, mask = mask}
	}
	unreachable()
}

@(private, not_an_error)
No_Pending_Signal :: struct #all_or_none {}

@(private, not_an_error)
Pending_Signal :: struct #all_or_none {
	signal: posix.Signal,
}

@(private)
Pending_Read :: union #no_nil {
	No_Pending_Signal,
	Pending_Signal,
	Supervision_Refusal,
}

@(private, require_results)
take_pending :: proc(signals: []posix.Signal) -> Pending_Read {
	pending := posix.sigset_t{}
	if posix.sigpending(&pending) != .OK {
		return Supervision_Refusal(Pending_Signals_Not_Read{cause = posix.errno()})
	}
	for signal in signals {
		membership := posix.sigismember(&pending, signal)
		if membership == -1 {
			return Supervision_Refusal(Signal_Membership_Not_Read{signal = signal, mask = pending, cause = posix.errno()})
		}
		if membership == 1 {
			one := posix.sigset_t{}
			_ = posix.sigemptyset(&one)
			_ = posix.sigaddset(&one, signal)
			received := posix.Signal.NONE
			cause := posix.sigwait(&one, &received)
			if cause != .NONE {
				return Supervision_Refusal(Signal_Not_Received{mask = one, cause = cause})
			}
			return Pending_Signal{signal = received}
		}
	}
	return No_Pending_Signal{}
}

@(private, require_results)
drain_control :: proc(record: ^$Record, observe: proc(^Record, Supervisor_Event)) -> Native_Change {
	for {
		switch pending in take_pending(control_signals[:]) {
		case No_Pending_Signal:
			return Native_Done{}
		case Supervision_Refusal:
			return pending
		case Pending_Signal:
			switch received in read_control_signal(pending.signal, control_mask()) {
			case Shutdown_Requested:
				observe(record, received)
			case Worker_Notified:
			case Signal_Outside_Set:
				return Supervision_Refusal(received)
			}
		}
	}
}
