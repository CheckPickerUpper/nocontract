package supervisor

import "core:mem"
import "base:runtime"
import "core:io"
import "core:os"
import "core:sys/posix"
import "core:sys/linux"
import "core:time"

// Normal_Exit preserves the worker's ordinary exit code, including nonzero codes.
@(not_an_error)
Normal_Exit :: struct #all_or_none {
	code: i32,
}

// Core_Dump distinguishes signal termination with and without a core dump.
Core_Dump :: enum {
	Not_Dumped,
	Dumped,
}

// Signaled preserves the signal and the kernel's core-dump classification.
Signaled :: struct #all_or_none {
	signal: posix.Signal,
	core:   Core_Dump,
}

// Termination_Kind separates an ordinary nonzero exit from a process crash.
Termination_Kind :: union #no_nil {
	Normal_Exit,
	Signaled,
}

// Termination retains both the unreaped native report and core's final process state.
Termination :: struct #all_or_none {
	report: posix.siginfo_t,
	state:  os.Process_State,
	kind:   Termination_Kind,
}

// Worker_Started records a worker re-executed to serve the supervisor's listening socket.
@(not_an_error)
Worker_Started :: struct #all_or_none {}

// Listening_Socket is the bound, listening socket the supervisor keeps open for its whole
// supervision and every worker inherits, and the port the kernel bound it to. Connections that
// arrive while a worker is replaced wait in its backlog.
Listening_Socket :: struct #all_or_none {
	descriptor: posix.FD,
	port:       u16,
}

// Worker_Ready reports the actual bound port received from a worker.
@(not_an_error)
Worker_Ready :: struct #all_or_none {
	port:    u16,
}

// Worker_Terminated keeps the complete native termination evidence.
Worker_Terminated :: struct #all_or_none {
	termination: Termination,
}

// Shutdown_Forwarded records a supervisor shutdown delivered to the current worker.
@(not_an_error)
Shutdown_Forwarded :: struct #all_or_none {
	signal:  posix.Signal,
}

// Worker_Lifecycle distinguishes the completed changes to a worker process.
Worker_Lifecycle :: union #no_nil {
	Worker_Started,
	Worker_Ready,
	Worker_Terminated,
	Shutdown_Forwarded,
}

// Worker_Event binds a lifecycle change to its exact process identity.
Worker_Event :: struct #all_or_none {
	process: os.Process,
	lifecycle: Worker_Lifecycle,
}

// Supervisor_Event keeps process observations separate from supervisor shutdown requests.
Supervisor_Event :: union #no_nil {
	Worker_Event,
	Shutdown_Requested,
}

// Supervision_Ended is the final worker death after a normal exit or requested shutdown.
@(not_an_error)
Supervision_Ended :: struct #all_or_none {
	termination: Termination,
}

// Worker_Reaped is a worker the supervisor killed, with the state it read when it waited for
// that exact process.
@(not_an_error)
Worker_Reaped :: struct #all_or_none {
	state: os.Process_State,
}

// Worker_Not_Killed keeps the kill that failed; the worker was not waited for.
Worker_Not_Killed :: struct #all_or_none {
	cause: Signal_Not_Sent,
}

// Worker_Not_Reaped keeps the wait that failed after the worker was killed.
Worker_Not_Reaped :: struct #all_or_none {
	cause: Cleanup_Child_Not_Reaped,
}

// Worker_Stop is how the supervisor released a worker it still owned.
Worker_Stop :: union #no_nil {
	Worker_Reaped,
	Worker_Not_Killed,
	Worker_Not_Reaped,
}

// Shutdown_Expired is a worker that did not finish within the shutdown period, and how the
// supervisor then stopped it.
Shutdown_Expired :: struct #all_or_none {
	expiry: Worker_Shutdown_Expired,
	stop:   Worker_Stop,
}

// Supervision_Completion records the primary result before native resources are released.
Supervision_Completion :: union #no_nil {
	Supervision_Ended,
	Shutdown_Period_Not_Positive,
	Shutdown_Expired,
	Supervision_Refusal,
}

// Supervision_Result also retains any failures while releasing the worker and signal mask.
Supervision_Result :: struct #all_or_none {
	completion: Supervision_Completion,
	cleanup:    [dynamic]Supervision_Refusal,
}

@(private, not_an_error)
No_Worker :: struct #all_or_none {}

@(private, not_an_error)
Awaiting_Readiness :: struct #all_or_none {}

@(private, not_an_error)
Reported_Port :: struct #all_or_none {
	port: u16,
}

// Readiness_Abandoned is a worker that closed its readiness pipe without reporting, which it
// does only by exiting.
@(private, not_an_error)
Readiness_Abandoned :: struct #all_or_none {}

@(private)
Worker_Readiness :: union #no_nil {
	Awaiting_Readiness,
	Reported_Port,
	Readiness_Abandoned,
}

@(private)
Worker_Lease :: struct #all_or_none {
	process:   os.Process,
	reader:    posix.FD,
}

@(private)
Live_Worker :: struct #all_or_none {
	lease: Worker_Lease,
	readiness: Worker_Readiness,
}

@(private)
Worker_Ownership :: union #no_nil {
	No_Worker,
	Worker_Lease,
}

@(private, not_an_error)
Running :: struct #all_or_none {}

@(private, not_an_error)
Stopping :: struct #all_or_none {
	signal: posix.Signal,
	period: time.Duration,
	deadline: time.Tick,
}

@(private)
Shutdown_Intent :: union #no_nil {
	Running,
	Stopping,
}

// supervise re-executes this program as a worker serving listener and bounds shutdown with a
// checked positive period. It replaces only a worker that reported ready and was terminated by a
// signal before shutdown was requested; a worker that dies before it is ready ends supervision.
// Call it before starting threads so workers inherit the blocked control signals.
@(require_results)
supervise :: proc(
	entry: Supervisor_Entry,
	listener: Listening_Socket,
	shutdown_period: time.Duration,
	record: ^$Record,
	observe: proc(^Record, Supervisor_Event),
	allocator: mem.Allocator,
) -> Supervision_Result {
	cleanup := make([dynamic]Supervision_Refusal, allocator)
	if shutdown_period <= 0 {
		return Supervision_Result{completion = Shutdown_Period_Not_Positive{period = shutdown_period}, cleanup = cleanup}
	}
	switch inherited in make_inheritable(listener.descriptor) {
	case Native_Done:
	case Supervision_Refusal:
		return Supervision_Result{completion = inherited, cleanup = cleanup}
	}
	mask := control_mask()
	original_mask := posix.sigset_t{}
	cause := posix.pthread_sigmask(.BLOCK, &mask, &original_mask)
	if cause != .NONE {
		return Supervision_Result{
			completion = Supervision_Refusal(Signal_Mask_Not_Changed{operation = .BLOCK, mask = mask, cause = cause}),
			cleanup = cleanup,
		}
	}
	ownership := Worker_Ownership(No_Worker{})
	supervised := supervise_workers(entry, listener, shutdown_period, original_mask, mask, &ownership, record, observe, allocator, &cleanup)
	completion := release_workers(supervised, ownership, &cleanup)
	switch drained in drain_control(record, observe) {
	case Native_Done:
	case Supervision_Refusal:
		append(&cleanup, drained)
	}
	restored := posix.pthread_sigmask(.SETMASK, &original_mask, nil)
	if restored != .NONE {
		append(&cleanup, Supervision_Refusal(Signal_Mask_Not_Changed{operation = .SETMASK, mask = original_mask, cause = restored}))
	}
	return Supervision_Result{completion = completion, cleanup = cleanup}
}

@(private, require_results)
release_workers :: proc(supervised: Workers_Supervised, ownership: Worker_Ownership, cleanup: ^[dynamic]Supervision_Refusal) -> Supervision_Completion {
	switch ended in supervised {
	case Owned_Worker_Expired:
		stop := stop_owned_worker(ended.lease.process)
		close_pipe(ended.lease.reader, cleanup)
		return Shutdown_Expired{expiry = ended.expiry, stop = stop}
	case Supervision_Ended:
		release_owned_worker(ownership, cleanup)
		return ended
	case Supervision_Refusal:
		release_owned_worker(ownership, cleanup)
		return ended
	}
	unreachable()
}

@(private)
release_owned_worker :: proc(ownership: Worker_Ownership, cleanup: ^[dynamic]Supervision_Refusal) {
	switch worker in ownership {
	case No_Worker:
	case Worker_Lease:
		switch stopped in stop_owned_worker(worker.process) {
		case Worker_Reaped:
		case Worker_Not_Killed:
			append(cleanup, Supervision_Refusal(stopped.cause))
		case Worker_Not_Reaped:
			append(cleanup, Supervision_Refusal(stopped.cause))
		}
		close_pipe(worker.reader, cleanup)
	}
}

// stop_owned_worker kills the worker the supervisor still owns and waits for that exact process,
// so no zombie outlives supervision.
@(private, require_results)
stop_owned_worker :: proc(process: os.Process) -> Worker_Stop {
	if posix.kill(posix.pid_t(process.pid), .SIGKILL) != .OK {
		return Worker_Not_Killed{cause = Signal_Not_Sent{process = posix.pid_t(process.pid), signal = .SIGKILL, cause = posix.errno()}}
	}
	state, wait_error := os.process_wait(process)
	switch _ in wait_error {
	case nil:
		return Worker_Reaped{state = state}
	case os.General_Error, io.Error, runtime.Allocator_Error, os.Platform_Error:
		return Worker_Not_Reaped{cause = Cleanup_Child_Not_Reaped{process = process, cause = wait_error}}
	}
	unreachable()
}

@(private)
Owned_Worker_Expired :: struct #all_or_none {
	expiry: Worker_Shutdown_Expired,
	lease:  Worker_Lease,
}

@(private)
Workers_Supervised :: union #no_nil {
	Supervision_Ended,
	Owned_Worker_Expired,
	Supervision_Refusal,
}

@(private, require_results)
supervise_workers :: proc(
	entry: Supervisor_Entry,
	listener: Listening_Socket,
	shutdown_period: time.Duration,
	original_mask, mask: posix.sigset_t,
	ownership: ^Worker_Ownership,
	record: ^$Record,
	observe: proc(^Record, Supervisor_Event),
	allocator: mem.Allocator,
	cleanup: ^[dynamic]Supervision_Refusal,
) -> Workers_Supervised {
	intent := Shutdown_Intent(Running{})
	for {
		switch launched in launch_worker(entry, listener.descriptor, original_mask, allocator, cleanup) {
		case Supervision_Refusal:
			return launched
		case Worker_Lease:
			ownership^ = launched
			observe(record, Worker_Event{process = launched.process, lifecycle = Worker_Started{}})
			switch stopped in wait_for_worker(launched, listener.port, &intent, mask, shutdown_period, record, observe, cleanup) {
			case Supervision_Refusal:
				return stopped
			case Worker_Shutdown_Expired:
				ownership^ = No_Worker{}
				return Owned_Worker_Expired{expiry = stopped, lease = launched}
			case Observed_Exit:
				ownership^ = No_Worker{}
				switch ended in stopped.completion {
				case Supervision_Refusal:
					return ended
				case Termination:
					switch _ in ended.kind {
					case Normal_Exit:
						return Supervision_Ended{termination = ended}
					case Signaled:
						switch _ in stopped.readiness {
						case Reported_Port:
						case Awaiting_Readiness, Readiness_Abandoned:
							// A replacement would start the same way and die the same way.
							return Supervision_Ended{termination = ended}
						}
						switch pending in take_pending([]posix.Signal{.SIGINT, .SIGTERM}) {
						case No_Pending_Signal:
						case Supervision_Refusal:
							return pending
						case Pending_Signal:
							observe(record, Shutdown_Requested{signal = pending.signal})
							return Supervision_Ended{termination = ended}
						}
						switch _ in intent {
						case Running:
						case Stopping:
							return Supervision_Ended{termination = ended}
						}
					}
				}
			}
		}
	}
}

@(private)
Worker_Wait :: union #no_nil {
	Observed_Exit,
	Worker_Shutdown_Expired,
	Supervision_Refusal,
}

@(private)
Exit_Completion :: union #no_nil {
	Termination,
	Supervision_Refusal,
}

@(private)
Observed_Exit :: struct #all_or_none {
	completion: Exit_Completion,
	readiness:  Worker_Readiness,
}

@(private, require_results)
wait_for_worker :: proc(
	lease: Worker_Lease,
	port: u16,
	intent: ^Shutdown_Intent,
	mask: posix.sigset_t,
	shutdown_period: time.Duration,
	record: ^$Record,
	observe: proc(^Record, Supervisor_Event),
	cleanup: ^[dynamic]Supervision_Refusal,
) -> Worker_Wait {
	worker := Live_Worker{lease = lease, readiness = Awaiting_Readiness{}}
	for {
		switch waited in wait_control(mask, lease.process, intent^) {
		case Supervision_Refusal:
			return waited
		case Worker_Shutdown_Expired:
			return waited
		case Control_Signal:
			switch wake in waited {
			case Signal_Outside_Set:
				return Supervision_Refusal(wake)
			case Shutdown_Requested:
				received_at := time.tick_now()
				observe(record, wake)
				switch _ in intent^ {
				case Running:
					intent^ = Stopping{signal = wake.signal, period = shutdown_period, deadline = time.tick_add(received_at, shutdown_period)}
					// odin-http's graceful callback belongs to the worker's main serving thread.
					cause := linux.tgkill(linux.Pid(lease.process.pid), linux.Pid(lease.process.pid), linux.Signal(wake.signal))
					if cause != .NONE {
						return Supervision_Refusal(Signal_Not_Sent{process = posix.pid_t(lease.process.pid), signal = wake.signal, cause = posix.Errno(cause)})
					}
					observe(record, Worker_Event{process = lease.process, lifecycle = Shutdown_Forwarded{signal = wake.signal}})
				case Stopping:
				}
			case Worker_Notified:
				// Read before reaping too: CHLD can precede the readiness notification.
				switch announced in read_worker_ready(worker, port, record, observe) {
				case Supervision_Refusal:
					return announced
				case Worker_Readiness:
					worker = Live_Worker{lease = lease, readiness = announced}
				}
				switch wake.kind {
				case .Readiness:
				case .Child:
					switch death in observe_child(lease.process) {
					case Supervision_Refusal:
						return death
					case Child_Alive:
					case Native_Death:
						state, wait_error := os.process_wait(lease.process)
						close_pipe(lease.reader, cleanup)
						switch _ in wait_error {
						case nil:
							termination := Termination{report = death.report, state = state, kind = death.kind}
							observe(record, Worker_Event{process = lease.process, lifecycle = Worker_Terminated{termination = termination}})
							return Observed_Exit{completion = termination, readiness = worker.readiness}
						case os.General_Error, io.Error, runtime.Allocator_Error, os.Platform_Error:
							return Observed_Exit{completion = Supervision_Refusal(Child_Not_Reaped{process = lease.process, report = death.report, cause = wait_error}), readiness = worker.readiness}
						}
					}
				}
			}
		}
	}
}
