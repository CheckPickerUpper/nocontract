package httpbinding

import "core:nbio"
import "core:net"
import "core:sys/linux"
import "core:sys/posix"

import http "../../dependencies/odin-http"

// Bound_Listener is the listening socket the supervisor binds once and every worker inherits.
Bound_Listener :: struct #all_or_none {
	socket:   net.TCP_Socket,
	endpoint: net.Endpoint,
}

// Opened_Listener confirms that odin-http owns the worker's copy of the inherited socket and one
// event-loop reference, and keeps the address the kernel bound.
Opened_Listener :: struct #all_or_none {
	endpoint: net.Endpoint,
}

// Listener_Socket_Not_Created keeps the requested address and native socket failure.
Listener_Socket_Not_Created :: struct #all_or_none {
	endpoint: net.Endpoint,
	cause: net.Create_Socket_Error,
	errno: linux.Errno,
}

// Listener_Option_Not_Set keeps the exact socket option and kernel error.
Listener_Option_Not_Set :: struct #all_or_none {
	socket: net.TCP_Socket,
	option: linux.Socket_Option,
	cause: linux.Errno,
}

// Listener_Not_Nonblocking retains the socket failure and its native errno.
Listener_Not_Nonblocking :: struct #all_or_none {
	socket: net.TCP_Socket,
	cause: net.Set_Blocking_Error,
	errno: linux.Errno,
}

// Listener_Not_Bound keeps the socket, requested endpoint and native bind failure.
Listener_Not_Bound :: struct #all_or_none {
	socket: net.TCP_Socket,
	endpoint: net.Endpoint,
	cause: net.Bind_Error,
	errno: linux.Errno,
}

// Listener_Not_Listening keeps the socket and kernel listen failure.
Listener_Not_Listening :: struct #all_or_none {
	socket: net.TCP_Socket,
	cause: linux.Errno,
}

// Listener_Endpoint_Not_Read keeps the socket whose bound address the kernel did not report.
Listener_Endpoint_Not_Read :: struct #all_or_none {
	socket: net.TCP_Socket,
	cause: net.Socket_Info_Error,
}

// Listener_Not_Adopted preserves the event-loop acquisition failure before socket transfer.
Listener_Not_Adopted :: struct #all_or_none {
	socket: net.TCP_Socket,
	cause: nbio.General_Error,
}

// Listener_Binding_Refusal is the exact operation that kept the supervisor from binding.
Listener_Binding_Refusal :: union #no_nil {
	Listener_Socket_Not_Created,
	Listener_Option_Not_Set,
	Listener_Not_Nonblocking,
	Listener_Not_Bound,
	Listener_Not_Listening,
	Listener_Endpoint_Not_Read,
}

// Listener_Binding is the supervisor's bound socket or the step that refused it.
Listener_Binding :: union #no_nil {
	Bound_Listener,
	Listener_Binding_Refusal,
}

// Listener_Adoption_Refusal is the exact operation that kept a worker from serving its socket.
Listener_Adoption_Refusal :: union #no_nil {
	Listener_Endpoint_Not_Read,
	Listener_Not_Adopted,
}

// Listener_Adoption records socket transfer to odin-http or the step that refused it.
Listener_Adoption :: union #no_nil {
	Opened_Listener,
	Listener_Adoption_Refusal,
}

// bind_listener binds and listens on one nonblocking socket before the first worker starts, so
// a bind failure belongs to the supervisor and every replacement serves the same socket.
@(require_results)
bind_listener :: proc(endpoint: net.Endpoint) -> Listener_Binding {
	socket, create_error := nbio.create_tcp_socket(net.family_from_endpoint(endpoint))
	if create_error != .None {
		return Listener_Binding_Refusal(Listener_Socket_Not_Created{endpoint = endpoint, cause = create_error, errno = linux.Errno(net.last_platform_error())})
	}
	blocking_error := net.set_blocking(socket, false)
	if blocking_error != .None {
		errno := linux.Errno(net.last_platform_error())
		net.close(socket)
		return Listener_Binding_Refusal(Listener_Not_Nonblocking{socket = socket, cause = blocking_error, errno = errno})
	}
	enabled := b32(true)
	option_cause := linux.setsockopt(linux.Fd(socket), linux.SOL_SOCKET, linux.Socket_Option.REUSEADDR, &enabled)
	if option_cause != .NONE {
		net.close(socket)
		return Listener_Binding_Refusal(Listener_Option_Not_Set{socket = socket, option = .REUSEADDR, cause = option_cause})
	}
	bind_error := net.bind(socket, endpoint)
	if bind_error != .None {
		errno := linux.Errno(net.last_platform_error())
		net.close(socket)
		return Listener_Binding_Refusal(Listener_Not_Bound{socket = socket, endpoint = endpoint, cause = bind_error, errno = errno})
	}
	listen_cause := linux.listen(linux.Fd(socket), 1000)
	if listen_cause != .NONE {
		net.close(socket)
		return Listener_Binding_Refusal(Listener_Not_Listening{socket = socket, cause = listen_cause})
	}
	bound, endpoint_error := net.bound_endpoint(socket)
	if endpoint_error != .None {
		net.close(socket)
		return Listener_Binding_Refusal(Listener_Endpoint_Not_Read{socket = socket, cause = endpoint_error})
	}
	return Bound_Listener{socket = socket, endpoint = bound}
}

// adopt_listener transfers a worker's inherited listening socket to odin-http.
// The caller keeps its creation loop acquired through this call and releases its own reference.
// On success, serve on this thread closes the worker's copy and releases odin-http's reference.
@(require_results)
adopt_listener :: proc(server: ^http.Server, socket: net.TCP_Socket, server_options: http.Server_Opts) -> Listener_Adoption {
	endpoint, endpoint_error := net.bound_endpoint(socket)
	if endpoint_error != .None {
		return Listener_Adoption_Refusal(Listener_Endpoint_Not_Read{socket = socket, cause = endpoint_error})
	}
	switch adopted in http.listen_on_socket(server, socket, server_options) {
	case http.Listening_On_Socket:
		return Opened_Listener{endpoint = endpoint}
	case http.Listener_Event_Loop_Unavailable:
		return Listener_Adoption_Refusal(Listener_Not_Adopted{socket = adopted.socket, cause = adopted.error})
	}
	unreachable()
}

// connection_socket borrows the HTTP library's live connection for a fatal response write.
@(require_results)
connection_socket :: proc(response: ^http.Response) -> posix.FD {
	return posix.FD(response._conn.socket)
}
