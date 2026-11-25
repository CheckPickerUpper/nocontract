#+build linux
#+test
package requestcrash

import "base:runtime"
import "core:c"
import "core:fmt"
import "core:mem"
import "core:strings"
import "core:sys/linux"
import "core:sys/posix"
import "core:testing"
import "core:time"
import "../requestlifetime"

@(private)
PANIC_FIELDS :: `"cause":"panic: route panic","stack":"`

@(private)
TRAP_FIELDS :: `"cause":"SIGILL","stack":null`

@(test)
panic_writes_prepared_reply_and_request_log :: proc(t: ^testing.T) {
	prove_crash(t, "panic", .Connected, .During_Request, crash_by_panic, PANIC_FIELDS)
}

@(test)
bounds_trap_writes_prepared_reply_and_request_log :: proc(t: ^testing.T) {
	prove_crash(t, "bounds", .Connected, .During_Request, crash_by_bounds, TRAP_FIELDS)
}

@(test)
closed_peer_keeps_the_request_log_and_original_signal :: proc(t: ^testing.T) {
	prove_crash(t, "closed peer", .Closed, .During_Request, crash_by_panic, PANIC_FIELDS)
}

@(test)
returned_dispatch_clears_the_borrowed_response_and_request_log :: proc(t: ^testing.T) {
	prove_crash(t, "completed", .Connected, .After_Request, crash_by_panic, PANIC_FIELDS)
}

@(test)
released_request_arena_does_not_release_the_thread_signal_stack :: proc(t: ^testing.T) {
	prove_crash(t, "released arena", .Connected, .After_Arena_Released, crash_by_panic, PANIC_FIELDS)
}

@(test)
returned_nested_dispatch_restores_the_enclosing_request :: proc(t: ^testing.T) {
	prove_crash(t, "enclosing request", .Connected, .During_Request, crash_after_nested_dispatch, PANIC_FIELDS)
}

@(test)
pending_lifetime_crash_writes_the_prepared_reply_and_request_log :: proc(t: ^testing.T) {
	started_at := time.tick_now()
	lifetime := requestlifetime.Request_Lifetime{
		deadline = time.tick_add(started_at, time.Second),
		duration = time.Second,
		completion = .Pending,
	}
	prove_crash(t, "pending lifetime", .Connected, .During_Request, crash_by_panic, PANIC_FIELDS, Lifetime_Dispatch{lifetime = &lifetime})
}

@(test)
claimed_deadline_crash_preserves_the_only_reply :: proc(t: ^testing.T) {
	started_at := time.tick_now()
	lifetime := requestlifetime.Request_Lifetime{
		deadline = time.tick_add(started_at, time.Second),
		duration = time.Second,
		completion = .Pending,
	}
	switch claimed in requestlifetime.claim_deadline(&lifetime).decision {
	case requestlifetime.Claimed:
		prove_crash(t, "claimed deadline", .Connected, .During_Request, crash_by_panic, PANIC_FIELDS, Lifetime_Dispatch{lifetime = &lifetime}, "prepared deadline reply\n")
	case requestlifetime.Already_Claimed:
		testing.expect(t, false, fmt.tprintf("deadline did not claim a fresh lifetime: %v", claimed))
	}
}

@(private)
Peer :: enum {
	Connected,
	Closed,
}

@(private)
Crash_Time :: enum {
	During_Request,
	After_Request,
	After_Arena_Released,
}

// prove_crash forks a child that dispatches one protected request on its end of a socket pair.
// The crash procedure receives that descriptor as its input, so a nested dispatch borrows the
// same connection without a descriptor number fixed across parallel tests.
@(private)
prove_crash :: proc(
	t: ^testing.T,
	name: string,
	peer: Peer,
	crash_time: Crash_Time,
	crash: proc(int) -> int,
	expected_fields: string,
	mode: Request_Protection_Mode = Standalone_Dispatch{},
	prior_response: string = "",
) {
	sockets := [2]posix.FD{}
	if !testing.expect_value(t, posix.socketpair(.UNIX, .STREAM, .IP, &sockets), .OK) {
		return
	}
	defer {
		switch peer {
		case .Connected:
			_ = posix.close(sockets[0])
		case .Closed:
		}
	}
	logs := [2]posix.FD{}
	if !testing.expect_value(t, posix.pipe(&logs), .OK) {
		_ = posix.close(sockets[1])
		return
	}
	defer _ = posix.close(logs[0])
	response := "HTTP/1.1 500 Internal Server Error\r\nContent-Type: application/json\r\nContent-Length: 55\r\nConnection: close\r\n\r\n{\"_tag\":\"RequestCrashed\",\"code\":\"NC-1172\",\"route\":null}"
	expected_response := response
	if prior_response != "" {
		expected_response = prior_response
	}
	diagnostic_prefix := fmt.aprintf(`{"event":"request_crashed","path":"/%s",`, name)
	defer delete(diagnostic_prefix)
	child_memory := make([]u8, 2 * posix.SIGSTKSZ)
	defer delete(child_memory)
	child_arena := mem.Arena{data = child_memory, offset = 0, peak_used = 0, temp_count = 0}
	switch peer {
	case .Connected:
	case .Closed:
		if !testing.expect_value(t, posix.shutdown(sockets[0], .RDWR), .OK) {
			_ = posix.close(sockets[0])
			_ = posix.close(sockets[1])
			_ = posix.close(logs[1])
			return
		}
		_ = posix.close(sockets[0])
	}
	child := posix.fork()
	switch {
	case child == -1:
		cause := posix.errno()
		_ = posix.close(sockets[1])
		_ = posix.close(logs[1])
		testing.expect(t, false, fmt.tprintf("fork failed: child=%v errno=%v", child, cause))
		return
	case child == 0:
		child_allocator := mem.arena_allocator(&child_arena)
		context.allocator = child_allocator
		context.temp_allocator = child_allocator
		context.assertion_failure_proc = runtime.default_assertion_failure_proc
		_ = posix.signal(.SIGPIPE, cast(proc "c" (posix.Signal))posix.SIG_DFL)
		// Linux's piped core collector ignores RLIMIT_CORE, so this child is not dumpable.
		PR_SET_DUMPABLE :: 4
		core_cause := linux.prctl(PR_SET_DUMPABLE, 0, 0, 0, 0)
		if core_cause != .NONE {
			fmt.eprintln("Could not disable test-child core collection:", core_cause, PR_SET_DUMPABLE)
			posix._exit(90)
		}
		if posix.dup2(logs[1], posix.STDERR_FILENO) != posix.STDERR_FILENO {
			fmt.eprintln("Could not attach test-child stderr:", logs[1], posix.STDERR_FILENO, posix.errno())
			posix._exit(96)
		}
		// Parallel tests' descriptors reach this child through fork; only its connection stays open.
		connection := linux.Fd(sockets[1])
		gaps := [2][2]linux.Fd{{posix.STDERR_FILENO + 1, connection - 1}, {connection + 1, max(linux.Fd)}}
		for gap in gaps {
			if gap[0] > gap[1] {
				continue
			}
			close_cause := linux.close_range(gap[0], gap[1], {})
			if close_cause != .NONE {
				fmt.eprintln("Could not close unrelated inherited test descriptors:", gap, close_cause)
				posix._exit(97)
			}
		}
		switch installed in install_handlers(context.allocator) {
		case Handlers_Installed:
		case Handler_Not_Installed:
			fmt.eprintln("Could not install request crash handlers:", installed)
			posix._exit(91)
		}
		if prior_response != "" {
			if posix.write(sockets[1], raw_data(prior_response), uint(len(prior_response))) != c.ssize_t(len(prior_response)) {
				fmt.eprintln("Could not write the preceding response:", posix.errno())
				posix._exit(100)
			}
		}
		protection := Request_Protection{socket = sockets[1], response = response, diagnostic_prefix = diagnostic_prefix, mode = mode}
		switch crash_time {
		case .During_Request:
			switch completed in answer_request(protection, int(sockets[1]), crash) {
			case Request_Answered(int):
				fmt.eprintln("Request did not crash:", completed)
				posix._exit(92)
			case Alternate_Stack_Not_Installed:
				fmt.eprintln("Could not prepare the request signal stack:", completed)
				posix._exit(93)
			}
		case .After_Request:
			switch completed in answer_request(protection, 7, answer_normally) {
			case Request_Answered(int):
				if completed.answer != 7 {
					fmt.eprintln("Request returned the wrong answer:", completed)
					posix._exit(94)
				}
			case Alternate_Stack_Not_Installed:
				fmt.eprintln("Could not prepare the completed request signal stack:", completed)
				posix._exit(93)
			}
			_ = crash(int(sockets[1]))
			fmt.eprintln("Completed request did not crash afterwards:", crash_time)
			posix._exit(95)
		case .After_Arena_Released:
			mapping := posix.mmap(nil, posix.SIGSTKSZ, {.READ, .WRITE}, {.ANONYMOUS, .PRIVATE})
			if mapping == posix.MAP_FAILED {
				fmt.eprintln("Request arena mmap failed:", posix.errno(), posix.SIGSTKSZ)
				posix._exit(98)
			}
			request_arena := mem.Arena{data = (cast([^]u8)mapping)[:posix.SIGSTKSZ], offset = 0, peak_used = 0, temp_count = 0}
			context.allocator = mem.arena_allocator(&request_arena)
			switch completed in answer_request(protection, 7, answer_normally) {
			case Request_Answered(int):
				if completed.answer != 7 {
					fmt.eprintln("First request returned the wrong answer:", completed)
					posix._exit(94)
				}
			case Alternate_Stack_Not_Installed:
				fmt.eprintln("First request signal stack could not be installed:", completed)
				posix._exit(93)
			}
			context.allocator = child_allocator
			if posix.munmap(mapping, posix.SIGSTKSZ) != .OK {
				fmt.eprintln("Request arena munmap failed:", posix.errno(), mapping, posix.SIGSTKSZ)
				posix._exit(99)
			}
			switch completed in answer_request(protection, int(sockets[1]), crash) {
			case Request_Answered(int):
				fmt.eprintln("Second request did not crash:", completed)
				posix._exit(92)
			case Alternate_Stack_Not_Installed:
				fmt.eprintln("Second request signal stack could not be installed:", completed)
				posix._exit(93)
			}
		}
	case:
		_ = posix.close(sockets[1])
		_ = posix.close(logs[1])
	}
	status := c.int(0)
	if !testing.expect_value(t, posix.waitpid(child, &status, {}), child) {
		return
	}
	log_buffer := make([]u8, 4096)
	defer delete(log_buffer)
	logged := posix.read(logs[0], raw_data(log_buffer), uint(len(log_buffer)))
	if logged == -1 {
		cause := posix.errno()
		testing.expect(t, false, fmt.tprintf("read child stderr: pid=%v count=%v errno=%v", child, logged, cause))
		return
	}
	written := string(log_buffer[:logged])
	if !testing.expect(t, posix.WIFSIGNALED(status), fmt.tprintf("child=%v status=%v stderr=%s", child, status, written)) {
		return
	}
	testing.expect_value(t, posix.WTERMSIG(status), posix.Signal.SIGILL)
	response_buffer := make([]u8, len(expected_response) + len(response) + 1)
	defer delete(response_buffer)
	switch peer {
	case .Connected:
		received := posix.read(sockets[0], raw_data(response_buffer), uint(len(response_buffer)))
		switch crash_time {
		case .During_Request, .After_Arena_Released:
			testing.expect_value(t, received, c.ssize_t(len(expected_response)))
			if received == c.ssize_t(len(expected_response)) {
				testing.expect_value(t, string(response_buffer[:received]), expected_response)
			}
		case .After_Request:
			testing.expect_value(t, received, c.ssize_t(0))
		}
		testing.expect_value(t, posix.read(sockets[0], raw_data(response_buffer), uint(len(response_buffer))), c.ssize_t(0))
	case .Closed:
	}
	switch crash_time {
	case .During_Request, .After_Arena_Released:
		if prior_response == "" {
			logged_line := strings.concatenate({diagnostic_prefix, expected_fields}, context.temp_allocator)
			testing.expect(t, strings.contains(written, logged_line), fmt.tprintf("stderr=%s", written))
			testing.expect(t, strings.has_suffix(written, "}\n"), fmt.tprintf("stderr=%s", written))
			testing.expect_value(t, strings.count(written, diagnostic_prefix), 1)
		}
		if prior_response != "" {
			testing.expect_value(t, strings.count(written, diagnostic_prefix), 0)
		}
	case .After_Request:
		testing.expect_value(t, strings.count(written, diagnostic_prefix), 0)
	}
}

@(private)
answer_normally :: proc(value: int) -> int {
	return value
}

@(private)
crash_by_panic :: proc(_: int) -> int {
	panic("route panic")
}

@(private)
crash_by_bounds :: proc(index: int) -> int {
	return bounds_in_contextless_dispatch(index)
}

@(private)
bounds_in_contextless_dispatch :: proc "contextless" (index: int) -> int {
	values := []int{}
	return values[index]
}

@(private)
crash_after_nested_dispatch :: proc(connection: int) -> int {
	protection := Request_Protection{socket = posix.FD(connection), response = "nested reply\n", diagnostic_prefix = `{"event":"nested request",`, mode = Standalone_Dispatch{}}
	switch answered in answer_request(protection, 7, answer_normally) {
	case Request_Answered(int):
		if answered.answer != 7 {
			fmt.eprintln("Nested request returned the wrong answer:", answered)
			posix._exit(94)
		}
	case Alternate_Stack_Not_Installed:
		fmt.eprintln("Nested request signal stack could not be prepared:", answered)
		posix._exit(93)
	}
	return crash_by_panic(connection)
}
