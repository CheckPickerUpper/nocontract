package supervisor

import "base:runtime"
import "core:c"
import "core:io"
import "core:mem"
import "core:os"
import "core:sys/posix"

@(private, not_an_error)
Native_Done :: struct #all_or_none {}

@(private)
Native_Change :: union #no_nil {
	Native_Done,
	Supervision_Refusal,
}

@(private)
Worker_Launch :: union #no_nil {
	Worker_Lease,
	Supervision_Refusal,
}

@(private)
Process_Launch :: union #no_nil {
	os.Process,
	Supervision_Refusal,
}

@(private, require_results)
launch_worker :: proc(entry: Supervisor_Entry, listener: posix.FD, original_mask: posix.sigset_t, allocator: mem.Allocator, cleanup: ^[dynamic]Supervision_Refusal) -> Worker_Launch {
	pipe := [2]posix.FD{}
	if posix.pipe(&pipe) != .OK {
		return Supervision_Refusal(Pipe_Not_Opened{cause = posix.errno()})
	}
	switch configured in configure_reader(pipe[0]) {
	case Supervision_Refusal:
		close_pipe(pipe[0], cleanup)
		close_pipe(pipe[1], cleanup)
		return configured
	case Native_Done:
	}
	worker := Worker_Entry{readiness = pipe[1], listener = listener, parent = posix.getpid(), original_mask = original_mask}
	started := start_worker(entry, worker, allocator)
	close_pipe(pipe[1], cleanup)
	switch started in started {
	case Supervision_Refusal:
		close_pipe(pipe[0], cleanup)
		return started
	case os.Process:
		return Worker_Lease{process = started, reader = pipe[0]}
	}
	unreachable()
}

@(private, require_results)
configure_reader :: proc(descriptor: posix.FD) -> Native_Change {
	settings := []struct #all_or_none { read: posix.FCNTL_Cmd, write: posix.FCNTL_Cmd, add: c.int }{
		{read = .GETFD, write = .SETFD, add = posix.FD_CLOEXEC},
		{read = .GETFL, write = .SETFL, add = posix.O_NONBLOCK},
	}
	for setting in settings {
		current := posix.fcntl(descriptor, setting.read)
		if current == -1 {
			return Supervision_Refusal(Pipe_Not_Configured{descriptor = descriptor, command = setting.read, argument = 0, cause = posix.errno()})
		}
		argument := current | setting.add
		if posix.fcntl(descriptor, setting.write, argument) == -1 {
			return Supervision_Refusal(Pipe_Not_Configured{descriptor = descriptor, command = setting.write, argument = argument, cause = posix.errno()})
		}
	}
	return Native_Done{}
}

// make_inheritable clears close-on-exec on the listening socket so every re-executed worker
// receives it.
@(private, require_results)
make_inheritable :: proc(descriptor: posix.FD) -> Native_Change {
	current := posix.fcntl(descriptor, .GETFD)
	if current == -1 {
		return Supervision_Refusal(Listener_Not_Inheritable{descriptor = descriptor, command = .GETFD, cause = posix.errno()})
	}
	if posix.fcntl(descriptor, .SETFD, current & ~c.int(posix.FD_CLOEXEC)) == -1 {
		return Supervision_Refusal(Listener_Not_Inheritable{descriptor = descriptor, command = .SETFD, cause = posix.errno()})
	}
	return Native_Done{}
}

@(private, require_results)
start_worker :: proc(entry: Supervisor_Entry, worker: Worker_Entry, allocator: mem.Allocator) -> Process_Launch {
	switch environment in worker_environment(entry.environment, worker, allocator) {
	case Supervision_Refusal:
		return environment
	case Environment_Assembled:
		defer free_environment(environment, allocator)
		process, start_error := os.process_start(os.Process_Desc{
			working_dir = "",
			command = entry.arguments,
			env = environment.values,
			stderr = os.stderr,
			stdout = os.stdout,
			stdin = os.stdin,
		})
		switch _ in start_error {
		case nil:
			return process
		case os.General_Error, io.Error, runtime.Allocator_Error, os.Platform_Error:
			return Supervision_Refusal(Worker_Not_Started{arguments = entry.arguments, worker = worker, cause = start_error})
		}
	}
	unreachable()
}

@(private)
close_pipe :: proc(descriptor: posix.FD, cleanup: ^[dynamic]Supervision_Refusal) {
	if posix.close(descriptor) != .OK {
		append(cleanup, Supervision_Refusal(Pipe_Not_Closed{descriptor = descriptor, cause = posix.errno()}))
	}
}

@(private, not_an_error)
Child_Alive :: struct #all_or_none {}

@(private)
Native_Death :: struct #all_or_none {
	report: posix.siginfo_t,
	kind:   Termination_Kind,
}

@(private)
Child_Observation :: union #no_nil {
	Child_Alive,
	Native_Death,
	Supervision_Refusal,
}

@(private, require_results)
observe_child :: proc(process: os.Process) -> Child_Observation {
	report := posix.siginfo_t{}
	if posix.waitid(.P_PID, posix.id_t(process.pid), &report, {.EXITED, .NOHANG, .NOWAIT}) != 0 {
		return Supervision_Refusal(Child_Not_Observed{process = process, cause = posix.errno()})
	}
	if report.si_pid == 0 {
		return Child_Alive{}
	}
	if report.si_pid != posix.pid_t(process.pid) {
		return Supervision_Refusal(Child_Report_Differed{process = process, report = report})
	}
	switch report.si_code.chld {
	case .EXITED:
		return Native_Death{report = report, kind = Normal_Exit{code = report.si_status}}
	case .KILLED:
		return Native_Death{report = report, kind = Signaled{signal = posix.Signal(report.si_status), core = .Not_Dumped}}
	case .DUMPED:
		return Native_Death{report = report, kind = Signaled{signal = posix.Signal(report.si_status), core = .Dumped}}
	case .TRAPPED, .STOPPED, .CONTINUED:
		return Supervision_Refusal(Child_Report_Differed{process = process, report = report})
	}
	unreachable()
}
