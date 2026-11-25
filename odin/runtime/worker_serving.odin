package nocontract

import "core:fmt"
import "core:net"
import "core:sys/posix"

import "httpbinding"
import "requestcrash"
import "supervisor"

// Supervision_Refused keeps the supervisor's failed step and any failure releasing its worker.
Supervision_Refused :: struct #all_or_none {
	cause:   supervisor.Supervision_Refusal,
	cleanup: []supervisor.Supervision_Refusal,
}

// Worker_Exited keeps the last worker's nonzero exit or signal death, which the supervisor does
// not replace, and any failure releasing supervision.
Worker_Exited :: struct #all_or_none {
	termination: supervisor.Termination,
	cleanup:     []supervisor.Supervision_Refusal,
}

// Shutdown_Period_Exceeded keeps the worker that outlived the shutdown period, how the
// supervisor stopped it, and any failure releasing supervision.
Shutdown_Period_Exceeded :: struct #all_or_none {
	expired: supervisor.Shutdown_Expired,
	cleanup: []supervisor.Supervision_Refusal,
}

// Supervision_Not_Released keeps the failures releasing supervision after its worker finished.
Supervision_Not_Released :: struct #all_or_none {
	cleanup: []supervisor.Supervision_Refusal,
}

@(private)
Supervised_Listener :: struct #all_or_none {
	options:   TCP_Listen_Options,
	bound:     net.Endpoint,
	listening: Listener_Endpoint,
}

@(private, require_results)
serve_supervised :: proc(options: Listen_Options, binding: Binding) -> Server_Terminal {
	switch entry in supervisor.enter(context.allocator) {
	case supervisor.Entry_Refusal:
		return Listener_Not_Opened{requested = Requested_Endpoint{value = options.tcp.endpoint}, failure = entry}
	case supervisor.Supervisor_Entry:
		return supervise_listener(entry, options.tcp)
	case supervisor.Worker_Entry:
		return serve_worker(entry, options.tcp, binding)
	}
	unreachable()
}

// supervise_listener binds the listening socket once, before the first worker, so a bind
// failure is the supervisor's and every replacement serves the socket the first one did.
@(private, require_results)
supervise_listener :: proc(entry: supervisor.Supervisor_Entry, options: TCP_Listen_Options) -> Server_Terminal {
	requested := Requested_Endpoint{value = options.endpoint}
	switch bound in httpbinding.bind_listener(options.endpoint) {
	case httpbinding.Listener_Binding_Refusal:
		return Listener_Not_Opened{requested = requested, failure = bound}
	case httpbinding.Bound_Listener:
		defer net.close(bound.socket)
		listener := Supervised_Listener{options = options, bound = bound.endpoint, listening = requested}
		socket := supervisor.Listening_Socket{descriptor = posix.FD(bound.socket), port = u16(bound.endpoint.port)}
		ending := supervisor.supervise(entry, socket, options.shutdown_period, &listener, observe_worker, context.allocator)
		return end_supervision(requested, listener.listening, ending)
	}
	unreachable()
}

@(private, require_results)
serve_worker :: proc(worker: supervisor.Worker_Entry, options: TCP_Listen_Options, binding: Binding) -> Server_Terminal {
	requested := Requested_Endpoint{value = options.endpoint}
	switch installed in requestcrash.install_handlers(context.allocator) {
	case requestcrash.Handlers_Installed:
	case requestcrash.Handler_Not_Installed:
		return Listener_Not_Opened{requested = requested, failure = installed}
	}
	switch prepared in prepare_crash_fallback() {
	case Crash_Diagnostic_Not_Encoded:
		return Listener_Not_Opened{requested = requested, failure = prepared}
	case Crash_Diagnostic_Fallback:
		return serve_tcp(worker, options, prepared, binding)
	}
	unreachable()
}

// end_supervision reports a failure as opening or serving by whether a worker had reported its
// bound address when it happened.
@(private, require_results)
end_supervision :: proc(requested: Requested_Endpoint, listening: Listener_Endpoint, ending: supervisor.Supervision_Result) -> Server_Terminal {
	cleanup := ending.cleanup[:]
	switch completion in ending.completion {
	case supervisor.Shutdown_Period_Not_Positive:
		return Listener_Not_Opened{requested = requested, failure = completion}
	case supervisor.Shutdown_Expired:
		return Listener_Not_Stopped{endpoint = listening, failure = Shutdown_Period_Exceeded{expired = completion, cleanup = cleanup}}
	case supervisor.Supervision_Refusal:
		return fail_supervision(listening, Supervision_Refused{cause = completion, cleanup = cleanup})
	case supervisor.Supervision_Ended:
		switch terminated in completion.termination.kind {
		case supervisor.Normal_Exit:
			if terminated.code != 0 {
				return fail_supervision(listening, Worker_Exited{termination = completion.termination, cleanup = cleanup})
			}
		case supervisor.Signaled:
			return fail_supervision(listening, Worker_Exited{termination = completion.termination, cleanup = cleanup})
		}
		if len(cleanup) != 0 {
			return Listener_Not_Stopped{endpoint = listening, failure = Supervision_Not_Released{cleanup = cleanup}}
		}
		delete(ending.cleanup)
		return Server_Stopped{endpoint = listening}
	}
	unreachable()
}

@(private, require_results)
fail_supervision :: proc(listening: Listener_Endpoint, failure: $Failure) -> Server_Terminal {
	switch endpoint in listening {
	case Requested_Endpoint:
		return Listener_Not_Opened{requested = endpoint, failure = failure}
	case Bound_Endpoint:
		return Listener_Not_Served{bound = endpoint, failure = failure}
	}
	unreachable()
}

@(private)
observe_worker :: proc(listener: ^Supervised_Listener, event: supervisor.Supervisor_Event) {
	switch event in event {
	case supervisor.Shutdown_Requested:
		fmt.eprintf("worker_shutdown signal=%v\n", event.signal)
	case supervisor.Worker_Event:
		switch change in event.lifecycle {
		case supervisor.Worker_Started:
			fmt.eprintf("worker_started pid=%v\n", event.process.pid)
		case supervisor.Worker_Ready:
			bound := Bound_Endpoint{value = listener.bound}
			listener^ = Supervised_Listener{options = listener.options, bound = listener.bound, listening = bound}
			fmt.eprintf("worker_ready pid=%v port=%v\n", event.process.pid, change.port)
			switch notice in listener.options.ready {
			case No_Ready_Notice:
			case Ready_Notice:
				notice.notify(bound.value)
			}
		case supervisor.Worker_Terminated:
			fmt.eprintf("worker_terminated pid=%v termination=%v\n", event.process.pid, change.termination.kind)
		case supervisor.Shutdown_Forwarded:
			fmt.eprintf("worker_shutdown_forwarded pid=%v signal=%v\n", event.process.pid, change.signal)
		}
	}
}
