#+build linux
package main

import "core:flags"
import "core:c"
import "core:encoding/endian"
import "core:fmt"
import "core:net"
import "core:os"
import "core:sys/linux"
import "core:sys/posix"
import "core:time"
import supervisor ".."

@(private)
Scenario :: enum {
	Restart,
	Startup,
	Startup_Crash,
	Normal_Nonzero,
	Stop_After_Crash,
	Final_Notification,
	Held_Shutdown,
	Invalid_Shutdown_Period,
}

@(private)
Configuration :: struct #all_or_none {
	scenario: Scenario     `args:"pos=0,required"`,
	signal:   posix.Signal `args:"pos=1,required"`,
	witness:  string       `args:"pos=2,required"`,
}

// A worker reads this environment entry to tell the first worker from its replacement.
@(private)
GENERATION :: "NOCONTRACT_PROOF_GENERATION"

@(private)
Observations :: struct #all_or_none {
	events:        [dynamic]supervisor.Supervisor_Event,
	configuration: Configuration,
	// The environment entry the next worker inherits, changed once the first worker is ready.
	generation:    ^string,
}

main :: proc() {
	configuration := Configuration{scenario = .Restart, signal = .SIGINT, witness = ""}
	flags.parse_or_exit(&configuration, os.args)
	if configuration.witness != "original argv" {
		fmt.eprintln("Original argv was not retained:", os.args)
		os.exit(1)
	}
	witness, inherited := os.lookup_env("NOCONTRACT_PROOF_ENV", context.allocator)
	if !inherited || witness != "inherited environment" {
		fmt.eprintln("Original environment was not retained:", witness, inherited)
		os.exit(1)
	}
	switch entry in supervisor.enter(context.allocator) {
	case supervisor.Entry_Refusal:
		fmt.eprintln("Entry refused:", entry)
		os.exit(1)
	case supervisor.Supervisor_Entry:
		supervise_scenario(entry, configuration)
	case supervisor.Worker_Entry:
		serve_scenario(entry, configuration)
	}
}

@(private)
supervise_scenario :: proc(entry: supervisor.Supervisor_Entry, configuration: Configuration) {
	listener, listen_error := net.listen_tcp(net.Endpoint{address = net.IP4_Loopback, port = 0})
	switch _ in listen_error {
	case nil:
	case net.Parse_Endpoint_Error, net.Create_Socket_Error, net.Dial_Error, net.Bind_Error, net.Listen_Error,
	     net.Accept_Error, net.TCP_Send_Error, net.UDP_Send_Error, net.TCP_Recv_Error, net.UDP_Recv_Error,
	     net.Shutdown_Error, net.Interfaces_Error, net.Socket_Info_Error, net.Socket_Option_Error,
	     net.Set_Blocking_Error, net.Resolve_Error, net.DNS_Error:
		fmt.eprintln("Supervisor listener failed:", listen_error)
		os.exit(1)
	}
	defer net.close(listener)
	endpoint, endpoint_error := net.bound_endpoint(listener)
	if endpoint_error != .None {
		fmt.eprintln("Bound supervisor address could not be read:", endpoint_error)
		os.exit(1)
	}
	environment := make([]string, len(entry.environment) + 1)
	defer delete(environment)
	copy(environment, entry.environment)
	environment[len(entry.environment)] = GENERATION + "=first"
	supervised := supervisor.Supervisor_Entry{arguments = entry.arguments, environment = environment}
	observations := Observations{events = make([dynamic]supervisor.Supervisor_Event), configuration = configuration, generation = &environment[len(entry.environment)]}
	defer delete(observations.events)
	shutdown_period := time.Second
	switch configuration.scenario {
	case .Restart, .Startup, .Startup_Crash, .Normal_Nonzero, .Stop_After_Crash, .Final_Notification:
	case .Held_Shutdown:
		shutdown_period = 10 * time.Millisecond
	case .Invalid_Shutdown_Period:
		shutdown_period = 0
	}
	socket := supervisor.Listening_Socket{descriptor = posix.FD(listener), port = u16(endpoint.port)}
	ending := supervisor.supervise(supervised, socket, shutdown_period, &observations, record_event, context.allocator)
	defer delete(ending.cleanup)
	if len(ending.cleanup) != 0 {
		fmt.eprintln("Supervisor cleanup failed:", ending.cleanup)
		os.exit(1)
	}
	switch completion in ending.completion {
	case supervisor.Supervision_Refusal:
		fmt.eprintln("Supervision refused:", completion)
		os.exit(1)
	case supervisor.Shutdown_Period_Not_Positive:
		prove_refused_period(completion, &observations, shutdown_period)
		return
	case supervisor.Shutdown_Expired:
		switch configuration.scenario {
		case .Held_Shutdown:
			prove_expired_worker(completion, &observations, shutdown_period)
			return
		case .Restart, .Startup, .Startup_Crash, .Normal_Nonzero, .Stop_After_Crash, .Final_Notification, .Invalid_Shutdown_Period:
			fmt.eprintln("An unexpected worker reached the shutdown deadline:", completion)
			os.exit(1)
		}
	case supervisor.Supervision_Ended:
		prove_ended_termination(completion, configuration)
	}
	starts := 0
	ports := make([dynamic]u16)
	defer delete(ports)
	deaths := make([dynamic]supervisor.Termination)
	defer delete(deaths)
	for event in observations.events {
		switch observed in event {
		case supervisor.Shutdown_Requested:
		case supervisor.Worker_Event:
			switch change in observed.lifecycle {
			case supervisor.Worker_Started:
				starts += 1
			case supervisor.Worker_Ready:
				append(&ports, change.port)
			case supervisor.Worker_Terminated:
				append(&deaths, change.termination)
			case supervisor.Shutdown_Forwarded:
				if change.signal != configuration.signal {
					fmt.eprintln("Wrong shutdown signal forwarded:", observed)
					os.exit(1)
				}
			}
		}
	}
	switch configuration.scenario {
	case .Restart:
		if starts != 2 || len(ports) != 2 || len(deaths) != 2 {
			fmt.eprintln("A crashed worker was not replaced once:", observations)
			os.exit(1)
		}
		if ports[0] != socket.port || ports[1] != socket.port {
			fmt.eprintln("A worker served another port than the supervisor's socket:", socket.port, ports)
			os.exit(1)
		}
		switch first in deaths[0].kind {
		case supervisor.Normal_Exit:
			fmt.eprintln("A signal death became a normal exit:", deaths[0])
			os.exit(1)
		case supervisor.Signaled:
			if first.signal != .SIGKILL {
				fmt.eprintln("The original death signal was lost:", deaths[0])
				os.exit(1)
			}
		}
	case .Startup_Crash:
		if starts != 1 || len(ports) != 0 || len(deaths) != 1 {
			fmt.eprintln("A worker that crashed before it was ready was replaced:", observations)
			os.exit(1)
		}
	case .Startup, .Normal_Nonzero, .Stop_After_Crash, .Final_Notification, .Held_Shutdown, .Invalid_Shutdown_Period:
		if starts != 1 || len(ports) != 1 || len(deaths) != 1 {
			fmt.eprintln("A stopped worker was restarted or its queued port was lost:", observations)
			os.exit(1)
		}
	}
	fmt.println("proved", configuration.scenario, configuration.signal, "starts", starts, "ports", ports)
}

@(private)
prove_ended_termination :: proc(completion: supervisor.Supervision_Ended, configuration: Configuration) {
	switch ended in completion.termination.kind {
	case supervisor.Signaled:
		switch configuration.scenario {
		case .Restart, .Startup, .Normal_Nonzero, .Final_Notification, .Held_Shutdown, .Invalid_Shutdown_Period:
			fmt.eprintln("Last worker was killed:", completion)
			os.exit(1)
		case .Startup_Crash:
			if ended.signal != .SIGSEGV {
				fmt.eprintln("The startup crash signal was lost:", completion)
				os.exit(1)
			}
		case .Stop_After_Crash:
			if ended.signal != .SIGKILL {
				fmt.eprintln("Original startup death was lost:", completion)
				os.exit(1)
			}
		}
	case supervisor.Normal_Exit:
		switch configuration.scenario {
		case .Restart, .Startup:
			if ended.code != i32(configuration.signal) {
				fmt.eprintln("Forwarded shutdown failed:", completion)
				os.exit(1)
			}
		case .Final_Notification:
			if ended.code != 0 {
				fmt.eprintln("Normal worker completion was lost:", completion)
				os.exit(1)
			}
		case .Normal_Nonzero:
			if ended.code != 7 {
				fmt.eprintln("Normal nonzero exit was not retained:", completion)
				os.exit(1)
			}
		case .Startup_Crash:
			fmt.eprintln("A startup crash became a normal exit:", completion)
			os.exit(1)
		case .Stop_After_Crash:
			fmt.eprintln("A replacement started after shutdown became pending:", completion)
			os.exit(1)
		case .Held_Shutdown, .Invalid_Shutdown_Period:
			fmt.eprintln("Supervision ended instead of preserving its shutdown refusal:", completion)
			os.exit(1)
		}
	}
}

@(private)
serve_scenario :: proc(entry: supervisor.Worker_Entry, configuration: Configuration) {
	// A second entry read sees no private controls after the first read cleared them.
	switch cleared in supervisor.enter(context.allocator) {
	case supervisor.Supervisor_Entry:
	case supervisor.Worker_Entry, supervisor.Entry_Refusal:
		fmt.eprintln("Worker control values were not cleared:", cleared)
		os.exit(1)
	}
	generation, _ := os.lookup_env(GENERATION, context.allocator)
	first := generation == "first"
	switch configuration.scenario {
	case .Startup:
		if posix.kill(entry.parent, configuration.signal) != .OK {
			fmt.eprintln("Startup shutdown could not be requested:", posix.errno())
			os.exit(1)
		}
		stop_mask := posix.sigset_t{}
		_ = posix.sigemptyset(&stop_mask)
		_ = posix.sigaddset(&stop_mask, configuration.signal)
		received := posix.Signal.NONE
		cause := posix.sigwait(&stop_mask, &received)
		if cause != .NONE || received != configuration.signal {
			fmt.eprintln("Shutdown was not forwarded while starting:", cause, received)
			os.exit(1)
		}
		if posix.raise(received) != .OK {
			fmt.eprintln("Forwarded shutdown could not remain pending:", posix.errno())
			os.exit(1)
		}
	case .Startup_Crash:
		// The deliberate crash leaves no core file behind.
		PR_SET_DUMPABLE :: 4
		_ = linux.prctl(PR_SET_DUMPABLE, 0, 0, 0, 0)
		_ = posix.raise(.SIGSEGV)
		os.exit(1)
	case .Restart, .Normal_Nonzero, .Stop_After_Crash, .Final_Notification, .Held_Shutdown, .Invalid_Shutdown_Period:
	}
	mask := posix.sigset_t{}
	_ = posix.sigemptyset(&mask)
	action := posix.sigaction_t{sa_handler = stop_worker, sa_mask = mask, sa_flags = {}, sa_restorer = nil}
	stop_signals := []posix.Signal{.SIGINT, .SIGTERM}
	for signal in stop_signals {
		if posix.sigaction(signal, &action, nil) != .OK {
			fmt.eprintln("Worker stop hook could not be installed:", signal, posix.errno())
			os.exit(1)
		}
	}
	endpoint, endpoint_error := net.bound_endpoint(net.TCP_Socket(entry.listener))
	if endpoint_error != .None {
		fmt.eprintln("The inherited listener's address could not be read:", endpoint_error)
		os.exit(1)
	}
	port := u16(endpoint.port)
	switch configuration.scenario {
	case .Normal_Nonzero:
		// No USR1 is sent: CHLD must still drain the atomic readiness message.
		packet := [size_of(u16)]u8{}
		_ = endian.put_u16(packet[:], .Big, port)
		if posix.write(entry.readiness, raw_data(packet[:]), size_of(port)) != size_of(port) {
			fmt.eprintln("Queued readiness write failed:", posix.errno())
			os.exit(1)
		}
		os.exit(7)
	case .Restart, .Startup, .Startup_Crash, .Stop_After_Crash, .Final_Notification, .Held_Shutdown, .Invalid_Shutdown_Period:
		ready_entry := entry
		switch configuration.scenario {
		case .Held_Shutdown:
			blocked := entry.original_mask
			for signal in stop_signals {
				_ = posix.sigaddset(&blocked, signal)
			}
			ready_entry = supervisor.Worker_Entry{readiness = entry.readiness, listener = entry.listener, parent = entry.parent, original_mask = blocked}
		case .Restart, .Startup, .Startup_Crash, .Normal_Nonzero, .Stop_After_Crash, .Final_Notification, .Invalid_Shutdown_Period:
		}
		switch ready in supervisor.report_ready(ready_entry, port) {
		case supervisor.Readiness_Report_Refusal:
			fmt.eprintln("Worker readiness failed:", ready)
			os.exit(1)
		case supervisor.Ready_Reported:
		}
	}
	switch configuration.scenario {
	case .Restart, .Stop_After_Crash:
		if first {
			_ = posix.raise(.SIGKILL)
			os.exit(1)
		}
		if posix.kill(entry.parent, configuration.signal) != .OK {
			fmt.eprintln("Replacement shutdown could not be requested:", posix.errno())
			os.exit(1)
		}
	case .Startup:
	case .Held_Shutdown:
		stop_mask := posix.sigset_t{}
		_ = posix.sigemptyset(&stop_mask)
		for signal in stop_signals {
			_ = posix.sigaddset(&stop_mask, signal)
		}
		received := posix.Signal.NONE
		cause := posix.sigwait(&stop_mask, &received)
		if cause != .NONE || received != configuration.signal {
			fmt.eprintln("The held worker did not receive its first shutdown:", cause, received)
			os.exit(1)
		}
	case .Final_Notification:
		os.exit(0)
	case .Normal_Nonzero, .Startup_Crash, .Invalid_Shutdown_Period:
		os.exit(1)
	}
	for {
		_ = posix.pause()
	}
}

@(private)
record_event :: proc(observations: ^Observations, event: supervisor.Supervisor_Event) {
	append(&observations.events, event)
	switch observed in event {
	case supervisor.Shutdown_Requested:
		switch observations.configuration.scenario {
		case .Held_Shutdown:
			// The second stop is pending before the supervisor next waits, so it is read during the
			// period and must not renew it.
			if count_shutdown_requests(observations) == 1 {
				_ = posix.raise(.SIGTERM)
			}
		case .Restart, .Startup, .Startup_Crash, .Normal_Nonzero, .Stop_After_Crash, .Final_Notification, .Invalid_Shutdown_Period:
		}
	case supervisor.Worker_Event:
		switch change in observed.lifecycle {
		case supervisor.Worker_Started, supervisor.Shutdown_Forwarded:
		case supervisor.Worker_Ready:
			observations.generation^ = GENERATION + "=replacement"
			switch observations.configuration.scenario {
			case .Held_Shutdown:
				_ = posix.raise(observations.configuration.signal)
			case .Restart, .Startup, .Startup_Crash, .Normal_Nonzero, .Stop_After_Crash, .Final_Notification, .Invalid_Shutdown_Period:
			}
		case supervisor.Worker_Terminated:
			switch observations.configuration.scenario {
			case .Restart, .Startup, .Startup_Crash, .Normal_Nonzero, .Held_Shutdown, .Invalid_Shutdown_Period:
			case .Stop_After_Crash:
				switch _ in change.termination.kind {
				case supervisor.Normal_Exit:
				case supervisor.Signaled:
					_ = posix.raise(observations.configuration.signal)
				}
			case .Final_Notification:
				_ = posix.raise(.SIGUSR1)
			}
		}
	}
}

@(private, require_results)
count_shutdown_requests :: proc(observations: ^Observations) -> int {
	count := 0
	for event in observations.events {
		switch _ in event {
		case supervisor.Shutdown_Requested:
			count += 1
		case supervisor.Worker_Event:
		}
	}
	return count
}

@(private)
stop_worker :: proc "c" (signal: posix.Signal) {
	posix._exit(i32(signal))
}

@(private)
prove_refused_period :: proc(refused: supervisor.Shutdown_Period_Not_Positive, observations: ^Observations, period: time.Duration) {
	switch observations.configuration.scenario {
	case .Invalid_Shutdown_Period:
		if refused.period != period || len(observations.events) != 0 {
			fmt.eprintln("An invalid period started a worker or lost the configured value:", refused, observations)
			os.exit(1)
		}
		fmt.println("proved", observations.configuration.scenario, refused)
	case .Restart, .Startup, .Startup_Crash, .Normal_Nonzero, .Stop_After_Crash, .Final_Notification, .Held_Shutdown:
		fmt.eprintln("A positive shutdown period was refused:", refused)
		os.exit(1)
	}
}

@(private)
prove_expired_worker :: proc(ended: supervisor.Shutdown_Expired, observations: ^Observations, period: time.Duration) {
	expired := ended.expiry
	started := make([dynamic]os.Process)
	defer delete(started)
	ready := make([dynamic]u16)
	defer delete(ready)
	requested := make([dynamic]posix.Signal)
	defer delete(requested)
	forwarded := make([dynamic]posix.Signal)
	defer delete(forwarded)
	for event in observations.events {
		switch observed in event {
		case supervisor.Shutdown_Requested:
			append(&requested, observed.signal)
		case supervisor.Worker_Event:
			switch change in observed.lifecycle {
			case supervisor.Worker_Started:
				append(&started, observed.process)
			case supervisor.Worker_Ready:
				append(&ready, change.port)
			case supervisor.Shutdown_Forwarded:
				append(&forwarded, change.signal)
			case supervisor.Worker_Terminated:
				fmt.eprintln("The held worker ended before the supervisor deadline:", observed)
				os.exit(1)
			}
		}
	}
	if len(started) != 1 || len(ready) != 1 || len(requested) != 2 || len(forwarded) != 1 {
		fmt.eprintln("Held-worker readiness, shutdown, or ownership differed:", observations)
		os.exit(1)
	}
	if expired.process.pid != started[0].pid || expired.signal != observations.configuration.signal || expired.period != period {
		fmt.eprintln("The shutdown refusal lost its worker, first signal, or period:", expired, observations)
		os.exit(1)
	}
	if requested[0] != observations.configuration.signal || requested[1] != .SIGTERM || forwarded[0] != observations.configuration.signal {
		fmt.eprintln("A later stop request changed the forwarded shutdown:", requested, forwarded)
		os.exit(1)
	}
	switch stopped in ended.stop {
	case supervisor.Worker_Reaped:
		if stopped.state.pid != expired.process.pid || !stopped.state.exited {
			fmt.eprintln("The supervisor waited for another process:", ended)
			os.exit(1)
		}
	case supervisor.Worker_Not_Killed, supervisor.Worker_Not_Reaped:
		fmt.eprintln("The expired worker was not stopped and reaped:", ended)
		os.exit(1)
	}
	status := c.int(0)
	if posix.waitpid(posix.pid_t(expired.process.pid), &status, {.NOHANG}) != -1 || posix.errno() != .ECHILD {
		fmt.eprintln("The exact expired worker was not reaped:", expired.process, status, posix.errno())
		os.exit(1)
	}
	if posix.kill(posix.pid_t(expired.process.pid), .NONE) != .FAIL || posix.errno() != .ESRCH {
		fmt.eprintln("The expired worker's process still exists:", expired.process, posix.errno())
		os.exit(1)
	}
	fmt.println("proved", observations.configuration.scenario, expired, "ready", ready, "requests", requested, "forwarded", forwarded, "waitpid=ECHILD kill=ESRCH")
}
