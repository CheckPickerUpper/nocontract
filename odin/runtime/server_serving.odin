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
import "requestcrash"
import "supervisor"

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
	crash_connection: Crash_Connection,
}

// Binding is the generated dispatcher that returns only typed route answers and refusals.
Binding :: struct #all_or_none {
	select: proc(request: Request) -> Route_Plan,
	deadline_exceeded: proc(route: string, timeout_milliseconds: u32) -> Answer,
	completion_unavailable: proc(route: string, operation: string, errno: i32) -> Answer,
	protection_unavailable: proc(refusal: Request_Protection_Unavailable) -> Answer,
}

// Request_Protection_Unavailable is a request thread whose crash signal stack was not installed.
Request_Protection_Unavailable :: requestcrash.Alternate_Stack_Not_Installed

@(private, not_an_error)
No_Ready_Notice :: struct #all_or_none {}

@(private)
Ready_Notice :: struct #all_or_none {
	notify: proc(endpoint: net.Endpoint),
}

// Listener_Notice is the application's observer of the address its first ready worker serves.
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
	body_limit: Request_Body_Limit,
	shutdown_period: time.Duration,
}

@(private)
Checked_Body_Limit :: struct #all_or_none {
	max_length: int,
}

// Request_Body_Limit contains only a nonnegative native cap constructed by this package.
Request_Body_Limit :: union #no_nil {
	Empty_Body_Only,
	Checked_Body_Limit,
}

// Empty_Body_Only accepts no payload bytes.
Empty_Body_Only :: struct #all_or_none {}

// Invalid_Request_Body_Limit retains the configured cap that is below zero.
Invalid_Request_Body_Limit :: struct #all_or_none {
	bytes: i64,
}

// Body_Limit_Parsing is a usable native limit or its invalid configuration.
Body_Limit_Parsing :: union #no_nil {
	Request_Body_Limit,
	Invalid_Request_Body_Limit,
}

// parse_body_limit accepts zero through the signed 64-bit maximum.
@(require_results)
parse_body_limit :: proc(bytes: i64) -> Body_Limit_Parsing {
	if bytes < 0 {
		return Invalid_Request_Body_Limit{bytes = bytes}
	}
	if bytes == 0 {
		return Request_Body_Limit(Empty_Body_Only{})
	}
	return Request_Body_Limit(Checked_Body_Limit{max_length = int(bytes)})
}

@(private, require_results)
body_max_length :: proc(limit: Request_Body_Limit) -> int {
	switch checked in limit {
	case Empty_Body_Only:
		return 0
	case Checked_Body_Limit:
		return checked.max_length
	}
	unreachable()
}

// Listen_Options is the closed listener configuration, built through listen_on.
Listen_Options :: struct #all_or_none {
	tcp: TCP_Listen_Options,
}

// listen_on selects the TCP endpoint a generated server listens on.
@(require_results)
listen_on :: proc(endpoint: net.Endpoint) -> Listen_Options {
	return Listen_Options{tcp = TCP_Listen_Options{endpoint = endpoint, ready = No_Ready_Notice{}, layers = {}, deadline = No_Deadline{}, body_limit = Request_Body_Limit(Checked_Body_Limit{max_length = 8_000_000}), shutdown_period = 5 * time.Second}}
}

// notify_when_ready observes the actual bound endpoint without exposing the socket or handler.
@(require_results)
notify_when_ready :: proc(options: Listen_Options, notify: proc(endpoint: net.Endpoint)) -> Listen_Options {
	return Listen_Options{tcp = TCP_Listen_Options{endpoint = options.tcp.endpoint, ready = Ready_Notice{notify = notify}, layers = options.tcp.layers, deadline = options.tcp.deadline, body_limit = options.tcp.body_limit, shutdown_period = options.tcp.shutdown_period}}
}

// with_layers selects ordered request and answer observers for the listener.
@(require_results)
with_layers :: proc(options: Listen_Options, layers: []requestlayers.Layer) -> Listen_Options {
	return Listen_Options{tcp = TCP_Listen_Options{endpoint = options.tcp.endpoint, ready = options.tcp.ready, layers = slice.clone(layers), deadline = options.tcp.deadline, body_limit = options.tcp.body_limit, shutdown_period = options.tcp.shutdown_period}}
}

// with_body_limit chooses the checked cap passed to odin-http before body decoding.
@(require_results)
with_body_limit :: proc(options: Listen_Options, limit: Request_Body_Limit) -> Listen_Options {
	return Listen_Options{tcp = TCP_Listen_Options{endpoint = options.tcp.endpoint, ready = options.tcp.ready, layers = options.tcp.layers, deadline = options.tcp.deadline, body_limit = limit, shutdown_period = options.tcp.shutdown_period}}
}

// with_shutdown_period selects the period the supervisor validates before starting a worker.
@(require_results)
with_shutdown_period :: proc(options: Listen_Options, period: time.Duration) -> Listen_Options {
	return Listen_Options{tcp = TCP_Listen_Options{endpoint = options.tcp.endpoint, ready = options.tcp.ready, layers = options.tcp.layers, deadline = options.tcp.deadline, body_limit = options.tcp.body_limit, shutdown_period = period}}
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

// Listener_Endpoint is the address a listener had reached when it stopped or failed: the
// requested one until a worker reports the address the kernel bound.
Listener_Endpoint :: union #no_nil {
	Requested_Endpoint,
	Bound_Endpoint,
}

// Server_Stopped is a generated server whose listener has finished on its serving thread.
@(not_an_error)
Server_Stopped :: struct #all_or_none {
	endpoint: Listener_Endpoint,
}

// Event_Loop_Not_Acquired preserves a failed event loop acquisition by a worker's main thread or
// by one of the serving threads odin-http starts.
Event_Loop_Not_Acquired :: struct #all_or_none {
	cause: nbio.General_Error,
}

// Server_Thread_Not_Spawned preserves odin-http's failure to create one of a worker's serving
// threads.
Server_Thread_Not_Spawned :: struct #all_or_none {
	cause: http.Server_Thread_Error,
}

// Listener_Opening_Failure is the step that kept a listener from reporting its bound address.
Listener_Opening_Failure :: union #no_nil {
	httpbinding.Listener_Binding_Refusal,
	httpbinding.Listener_Adoption_Refusal,
	httpbinding.Shutdown_Handler_Not_Installed,
	requestcrash.Handler_Not_Installed,
	supervisor.Entry_Refusal,
	supervisor.Readiness_Report_Refusal,
	supervisor.Shutdown_Period_Not_Positive,
	Crash_Diagnostic_Not_Encoded,
	Event_Loop_Not_Acquired,
	Supervision_Refused,
	Worker_Exited,
}

// Listener_Not_Opened preserves the chosen endpoint and the step that kept it from opening.
@(error_code = "NC-1140")
Listener_Not_Opened :: struct #all_or_none {
	requested: Requested_Endpoint,
	failure:   Listener_Opening_Failure,
}

// Listener_Serving_Failure is what stopped a listener after it reported its bound address.
Listener_Serving_Failure :: union #no_nil {
	Event_Loop_Not_Acquired,
	Network_Failure,
	Server_Thread_Not_Spawned,
	Supervision_Refused,
	Worker_Exited,
}

// Listener_Not_Served preserves the bound endpoint and the failure that stopped serving.
@(error_code = "NC-1141")
Listener_Not_Served :: struct #all_or_none {
	bound:   Bound_Endpoint,
	failure: Listener_Serving_Failure,
}

// Listener_Stopping_Failure is why a stopping listener could not finish.
Listener_Stopping_Failure :: union #no_nil {
	Shutdown_Period_Exceeded,
	Supervision_Not_Released,
}

// Listener_Not_Stopped preserves the listener's endpoint and why its shutdown did not finish.
@(error_code = "NC-1142")
Listener_Not_Stopped :: struct #all_or_none {
	endpoint: Listener_Endpoint,
	failure:  Listener_Stopping_Failure,
}

// Server_Terminal is the end of a generated server's blocking serve operation.
Server_Terminal :: union #no_nil {
	Server_Stopped,
	Listener_Not_Opened,
	Listener_Not_Served,
	Listener_Not_Stopped,
}

// serve supervises the listener and waits for its worker's shutdown.
@(require_results)
serve :: proc(options: Listen_Options, binding: Binding) -> Server_Terminal {
	return serve_supervised(options, binding)
}

@(private, require_results)
serve_tcp :: proc(worker: supervisor.Worker_Entry, options: TCP_Listen_Options, fallback: Crash_Diagnostic_Fallback, binding: Binding) -> Server_Terminal {
	cause := nbio.acquire_thread_event_loop()
	switch cause {
	case .None:
		defer nbio.release_thread_event_loop()
		return serve_on_event_loop(worker, options, fallback, binding)
	case .Allocation_Failed, .Unsupported:
	}
	// General_Error also carries the platform errno returned by the event-loop setup.
	return Listener_Not_Opened{requested = Requested_Endpoint{value = options.endpoint}, failure = Event_Loop_Not_Acquired{cause = cause}}
}

@(private, require_results)
serve_on_event_loop :: proc(worker: supervisor.Worker_Entry, options: TCP_Listen_Options, fallback: Crash_Diagnostic_Fallback, binding: Binding) -> Server_Terminal {
	// odin-http's interrupt registration borrows this server for the process lifetime.
	server := new(http.Server)
	server_options := http.Server_Opts{
		auto_expect_continue = http.Default_Server_Opts.auto_expect_continue,
		redirect_head_to_get = false,
		limit_request_line = http.Default_Server_Opts.limit_request_line,
		limit_headers = http.Default_Server_Opts.limit_headers,
		thread_count = http.Default_Server_Opts.thread_count,
	}
	endpoint := options.endpoint
	switch opened in httpbinding.adopt_listener(server, net.TCP_Socket(worker.listener), server_options) {
	case httpbinding.Opened_Listener:
		endpoint = opened.endpoint
	case httpbinding.Listener_Adoption_Refusal:
		free(server)
		return Listener_Not_Opened{requested = Requested_Endpoint{value = options.endpoint}, failure = opened}
	}
	switch installed in httpbinding.install_shutdown_handlers(server) {
	case httpbinding.Shutdown_Handlers_Installed:
	case httpbinding.Shutdown_Handler_Not_Installed:
		close_unserved_listener(server)
		return Listener_Not_Opened{requested = Requested_Endpoint{value = options.endpoint}, failure = installed}
	}
	// The worker reports ready before serve starts any thread that accepts, so the supervisor
	// replaces a worker that crashes on its first request instead of ending supervision.
	switch ready in supervisor.report_ready(worker, u16(endpoint.port)) {
	case supervisor.Ready_Reported:
	case supervisor.Readiness_Report_Refusal:
		close_unserved_listener(server)
		return Listener_Not_Opened{requested = Requested_Endpoint{value = options.endpoint}, failure = ready}
	}
	bound := Serving_API{binding = binding, layers = options.layers, deadline = options.deadline, allocator = context.allocator, body_limit = options.body_limit, crash_fallback = fallback}
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
				httpbinding.receive_body(request, body_max_length(binding.body_limit), pending, answer_received_request)
			case nil:
				respond(response, binding, Request{path = request.url.path, query = request.url.query, headers = request.headers._kv, content = Request_Line_Not_Received{}, lifetime = requestlifetime.No_Request_Deadline{}, crash_connection = Crash_Connection{socket = httpbinding.connection_socket(response), fallback = binding.crash_fallback}}, started)
			}
		},
	}
	switch cause in http.serve(server, handler) {
	case nil:
		return Server_Stopped{endpoint = Bound_Endpoint{value = endpoint}}
	case net.Network_Error:
		return Listener_Not_Served{bound = Bound_Endpoint{value = endpoint}, failure = Network_Failure{cause = cause}}
	case http.Server_Thread_Error:
		return Listener_Not_Served{bound = Bound_Endpoint{value = endpoint}, failure = Server_Thread_Not_Spawned{cause = cause}}
	case nbio.General_Error:
		return Listener_Not_Served{bound = Bound_Endpoint{value = endpoint}, failure = Event_Loop_Not_Acquired{cause = cause}}
	}
	unreachable()
}

// close_unserved_listener closes an adopted listener that will not serve. Only serve on the
// adopting thread closes its socket and releases odin-http's event loop reference, and after
// server_shutdown it returns at once with no error.
@(private)
close_unserved_listener :: proc(server: ^http.Server) {
	http.server_shutdown(server)
	_ = http.serve(server, {})
}

@(private)
Serving_API :: struct #all_or_none {
	binding: Binding,
	layers:  []requestlayers.Layer,
	deadline: Request_Deadline,
	allocator: mem.Allocator,
	body_limit: Request_Body_Limit,
	crash_fallback: Crash_Diagnostic_Fallback,
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
		crash_connection = Crash_Connection{socket = httpbinding.connection_socket(pending.response), fallback = pending.binding.crash_fallback},
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
			start_deadline_request(api, response, request, selected, plan.crash_response, visit, deadline, started)
			return
		case No_Deadline:
		}
	case Immediate_Answer:
	}
	answer := answer_through_layers(api.binding, api.layers, visit, plan, request)
	publish_answer(response, visit, answer, started)
}

@(private, require_results)
answer_through_layers :: proc(binding: Binding, layers: []requestlayers.Layer, visit: requestlayers.Visit, plan: Route_Plan, request: Request) -> Answer {
	dispatch := Layer_Dispatch{layers = layers, visit = visit, plan = plan, request = request}
	switch protected in protect_request(request, plan.crash_response, describe_visited_route(plan), dispatch, answer_layer_dispatch) {
	case requestcrash.Request_Answered(Answer):
		return protected.answer
	case requestcrash.Alternate_Stack_Not_Installed:
		log.errorf("Request signal stack installation failed: %v", protected)
		return binding.protection_unavailable(protected)
	}
	unreachable()
}

@(private)
Layer_Dispatch :: struct #all_or_none {
	layers: []requestlayers.Layer,
	visit: requestlayers.Visit,
	plan: Route_Plan,
	request: Request,
}

@(private, require_results)
answer_layer_dispatch :: proc(dispatch: Layer_Dispatch) -> Answer {
	requestlayers.enter(dispatch.layers, dispatch.visit)
	answer := answer_planned_route(dispatch.plan, dispatch.request)
	answered := describe_answer(answer)
	requestlayers.leave(dispatch.layers, dispatch.visit, answered)
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
