package nocontract

#assert(ODIN_OS == .Linux, "NoContract's Odin server requires Linux. Odin's native event loop cannot cancel an accept and report whether it accepted a connection on this platform.")

import "core:nbio"
import "core:net"
import "core:log"
import "core:mem"
import "core:slice"
import "core:time"

import http "../dependencies/odin-http"
import "httpbinding"
import requestlayers "layers"
import "requestlog"
import "requestlifetime"

// Received_Request is a method and body odin-http read for a request.
Received_Request :: struct #all_or_none {
	method:  string,
	body:    httpbinding.Body_Receipt,
}

// Request_Line_Not_Received is a callback from the HTTP library with no request line.
Request_Line_Not_Received :: struct #all_or_none {}

// Request_Content preserves whether the HTTP library supplied a complete request line.
Request_Content :: union #no_nil {
	Received_Request,
	Request_Line_Not_Received,
}

// Request is the request data a generated route reads, without an HTTP response writer.
Request :: struct #all_or_none {
	path:    string,
	query:   string,
	headers: map[string]string,
	content: Request_Content,
	lifetime: requestlifetime.Handler_Lifetime,
}

// Binding is the generated dispatcher that returns only typed route answers and refusals.
Binding :: struct #all_or_none {
	select: proc(request: Request) -> Route_Plan,
	deadline_exceeded: proc(route: string, timeout_milliseconds: u32) -> Answer,
	completion_unavailable: proc(route: string, operation: string, errno: i32) -> Answer,
}

@(private, not_an_error)
No_Ready_Notice :: struct #all_or_none {}

@(private)
Ready_Notice :: struct #all_or_none {
	notify: proc(endpoint: net.Endpoint),
}

@(private)
Listener_Notice :: union #no_nil {
	No_Ready_Notice,
	Ready_Notice,
}

@(private)
TCP_Listen_Options :: struct #all_or_none {
	endpoint: net.Endpoint,
	ready:    Listener_Notice,
	layers:   []requestlayers.Layer,
	deadline: Request_Deadline,
}

// Listen_Options is the closed listener configuration, built through listen_on.
Listen_Options :: struct #all_or_none {
	tcp: TCP_Listen_Options,
}

// listen_on selects the TCP endpoint a generated server listens on.
@(require_results)
listen_on :: proc(endpoint: net.Endpoint) -> Listen_Options {
	return Listen_Options{tcp = TCP_Listen_Options{endpoint = endpoint, ready = No_Ready_Notice{}, layers = {}, deadline = No_Deadline{}}}
}

// notify_when_ready observes the actual bound endpoint without exposing the socket or handler.
@(require_results)
notify_when_ready :: proc(options: Listen_Options, notify: proc(endpoint: net.Endpoint)) -> Listen_Options {
	return Listen_Options{tcp = TCP_Listen_Options{endpoint = options.tcp.endpoint, ready = Ready_Notice{notify = notify}, layers = options.tcp.layers, deadline = options.tcp.deadline}}
}

// with_layers selects ordered request and answer observers for the listener.
@(require_results)
with_layers :: proc(options: Listen_Options, layers: []requestlayers.Layer) -> Listen_Options {
	return Listen_Options{tcp = TCP_Listen_Options{endpoint = options.tcp.endpoint, ready = options.tcp.ready, layers = slice.clone(layers), deadline = options.tcp.deadline}}
}

// Bound_Endpoint is the address assigned to a listener the runtime successfully opened.
Bound_Endpoint :: struct #all_or_none {
	value: net.Endpoint,
}

// Network_Failure preserves an HTTP library operation's original network failure.
Network_Failure :: struct #all_or_none {
	cause: net.Network_Error,
}

// Requested_Endpoint is the address an application asked the runtime to bind.
Requested_Endpoint :: struct #all_or_none {
	value: net.Endpoint,
}

// Server_Stopped is a generated server whose listener has finished on its serving thread.
@(not_an_error)
Server_Stopped :: struct #all_or_none {
	bound: Bound_Endpoint,
}

// Listener_Event_Loop_Not_Available preserves the first event-loop acquisition failure.
@(error_code = "NC-1163")
Listener_Event_Loop_Not_Available :: struct #all_or_none {
	requested: Requested_Endpoint,
	cause: nbio.General_Error,
}

// Listener_Not_Opened preserves the chosen endpoint and odin-http's original listen failure.
@(error_code = "NC-1140")
Listener_Not_Opened :: struct #all_or_none {
	requested: Requested_Endpoint,
	failure:   Network_Failure,
}

// Listener_Endpoint_Not_Read keeps a failed bound-address read and its shutdown result.
@(error_code = "NC-1160")
Listener_Endpoint_Not_Read :: struct #all_or_none {
	requested:      Requested_Endpoint,
	cause:          net.Socket_Info_Error,
	shutdown_cause: net.Shutdown_Error,
	cleanup_cause:  nbio.General_Error,
}

// Listener_Not_Served preserves the bound endpoint and odin-http's original serving failure.
@(error_code = "NC-1141")
Listener_Not_Served :: struct #all_or_none {
	bound:   Bound_Endpoint,
	failure: Network_Failure,
}

// Server_Terminal is the end of a generated server's blocking serve operation.
Server_Terminal :: union #no_nil {
	Server_Stopped,
	Listener_Event_Loop_Not_Available,
	Listener_Not_Opened,
	Listener_Endpoint_Not_Read,
	Listener_Not_Served,
}

// serve owns listen, the ready notice, and odin-http's event loop on the calling thread.
// The library's documented interrupt handler stops the listener and joins its workers.
@(require_results)
serve :: proc(options: Listen_Options, binding: Binding) -> Server_Terminal {
	return serve_tcp(options.tcp, binding)
}

@(private, require_results)
serve_tcp :: proc(options: TCP_Listen_Options, binding: Binding) -> Server_Terminal {
	cause := nbio.acquire_thread_event_loop()
	switch cause {
	case .None:
		defer nbio.release_thread_event_loop()
		return serve_on_event_loop(options, binding)
	case .Allocation_Failed, .Unsupported:
	}
	// General_Error also carries the platform errno returned by the event-loop setup.
	return Listener_Event_Loop_Not_Available{requested = Requested_Endpoint{value = options.endpoint}, cause = cause}
}

@(private, require_results)
serve_on_event_loop :: proc(options: TCP_Listen_Options, binding: Binding) -> Server_Terminal {
	// odin-http's interrupt registration borrows this server for the process lifetime.
	server := new(http.Server)
	server_options := http.Server_Opts{
		auto_expect_continue = http.Default_Server_Opts.auto_expect_continue,
		redirect_head_to_get = false,
		limit_request_line = http.Default_Server_Opts.limit_request_line,
		limit_headers = http.Default_Server_Opts.limit_headers,
		thread_count = http.Default_Server_Opts.thread_count,
	}
	switch cause in http.listen(server, options.endpoint, server_options) {
	case nil:
	case net.Create_Socket_Error, net.Dial_Error, net.Listen_Error, net.Accept_Error, net.Bind_Error,
		net.TCP_Send_Error, net.UDP_Send_Error, net.TCP_Recv_Error, net.UDP_Recv_Error, net.Shutdown_Error,
		net.Interfaces_Error, net.Socket_Info_Error, net.Socket_Option_Error, net.Set_Blocking_Error,
		net.Parse_Endpoint_Error, net.Resolve_Error, net.DNS_Error:
		free(server)
		return Listener_Not_Opened{requested = Requested_Endpoint{value = options.endpoint}, failure = Network_Failure{cause = cause}}
	}
	endpoint, endpoint_error := net.bound_endpoint(server.tcp_sock)
	switch endpoint_error {
	case .None:
	case .Network_Unreachable, .Insufficient_Resources, .Invalid_Argument, .Unsupported_Socket, .Connection_Closed, .Unknown:
		shutdown_error := net.shutdown(server.tcp_sock, .Both)
		net.close(server.tcp_sock)
		cleanup_error := nbio.run()
		nbio.release_thread_event_loop()
		free(server)
		return Listener_Endpoint_Not_Read{requested = Requested_Endpoint{value = options.endpoint}, cause = endpoint_error, shutdown_cause = shutdown_error, cleanup_cause = cleanup_error}
	}
	http.server_shutdown_on_interrupt(server)
	switch notice in options.ready {
	case No_Ready_Notice:
	case Ready_Notice:
		notice.notify(endpoint)
	}
	bound := Serving_API{binding = binding, layers = options.layers, deadline = options.deadline, allocator = context.allocator}
	handler := http.Handler{
		user_data = &bound,
		next = {},
		handle = proc(handler: ^http.Handler, request: ^http.Request, response: ^http.Response) {
			binding := cast(^Serving_API)handler.user_data
			started := time.tick_now()
			switch line in request.line {
			case http.Requestline:
				pending := new_clone(
					Pending_Request{binding = binding, method = http.method_string(line.method), request = request, response = response, started = started},
					context.temp_allocator,
				)
				httpbinding.receive_body(request, pending, answer_received_request)
			case nil:
				respond(response, binding, Request{path = request.url.path, query = request.url.query, headers = request.headers._kv, content = Request_Line_Not_Received{}, lifetime = requestlifetime.No_Request_Deadline{}}, started)
			}
		},
	}
	switch cause in http.serve(server, handler) {
	case nil:
		return Server_Stopped{bound = Bound_Endpoint{value = endpoint}}
	case net.Create_Socket_Error, net.Dial_Error, net.Listen_Error, net.Accept_Error, net.Bind_Error,
		net.TCP_Send_Error, net.UDP_Send_Error, net.TCP_Recv_Error, net.UDP_Recv_Error, net.Shutdown_Error,
		net.Interfaces_Error, net.Socket_Info_Error, net.Socket_Option_Error, net.Set_Blocking_Error,
		net.Parse_Endpoint_Error, net.Resolve_Error, net.DNS_Error:
		return Listener_Not_Served{bound = Bound_Endpoint{value = endpoint}, failure = Network_Failure{cause = cause}}
	}
	unreachable()
}

@(private)
Serving_API :: struct #all_or_none {
	binding: Binding,
	layers:  []requestlayers.Layer,
	deadline: Request_Deadline,
	allocator: mem.Allocator,
}

@(private)
Pending_Request :: struct #all_or_none {
	binding:  ^Serving_API,
	method:   string,
	request:  ^http.Request,
	response: ^http.Response,
	started:  time.Tick,
}

@(private)
answer_received_request :: proc(pending: ^Pending_Request, receipt: httpbinding.Body_Receipt) {
	context.allocator = context.temp_allocator
	request := Request{
		path = pending.request.url.path,
		query = pending.request.url.query,
		headers = pending.request.headers._kv,
		content = Received_Request{method = pending.method, body = receipt},
		lifetime = requestlifetime.No_Request_Deadline{},
	}
	respond(pending.response, pending.binding, request, pending.started)
}

@(private)
respond :: proc(response: ^http.Response, api: ^Serving_API, request: Request, started: time.Tick) {
	plan := api.binding.select(request)
	visit := visit_request(plan, request.path)
	switch selected in plan.selection {
	case Selected_Route:
		switch deadline in api.deadline {
		case Configured_Deadline:
			start_deadline_request(api, response, request, selected, visit, deadline, started)
			return
		case No_Deadline:
		}
	case Immediate_Answer:
	}
	answer := answer_through_layers(api.layers, visit, plan, request)
	publish_answer(response, visit, answer, started)
}

@(private, require_results)
answer_through_layers :: proc(layers: []requestlayers.Layer, visit: requestlayers.Visit, plan: Route_Plan, request: Request) -> Answer {
	requestlayers.enter(layers, visit)
	answer := answer_planned_route(plan, request)
	answered := describe_answer(answer)
	requestlayers.leave(layers, visit, answered)
	return answer
}

@(private)
publish_answer :: proc(response: ^http.Response, visit: requestlayers.Visit, answer: Answer, started: time.Tick) {
	answered := describe_answer(answer)
	switch emitted in requestlog.log_request(requestlog.Request_Log{route = visit.route, method = visit.method, status = answered.status, code = answered.code, duration = time.tick_since(started)}) {
	case requestlog.Request_Logged:
	case requestlog.Request_Log_Not_Encoded:
		log.errorf("NoContract could not encode its request log: %v", emitted)
	}
	write_answer(response, answer)
}
