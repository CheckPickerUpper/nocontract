package nocontract

import "core:sys/posix"

import "requestcrash"
import "requestlifetime"

// Crash_Connection is what a request's crash path borrows while a generated request runs: the
// connection its prepared answer goes to, and the crash-log prefix the worker encoded at startup
// for a request whose own prefix cannot be encoded.
Crash_Connection :: struct #all_or_none {
	socket:   posix.FD,
	fallback: Crash_Diagnostic_Fallback,
}

// Crash_Diagnostic_Fallback is the fixed crash-log prefix naming no request fields.
Crash_Diagnostic_Fallback :: struct #all_or_none {
	prefix: string,
}

// Crash_Diagnostic_Not_Encoded keeps the encoder failure for the fixed crash-log prefix.
Crash_Diagnostic_Not_Encoded :: struct #all_or_none {
	failure: JSON_Encode_Failure,
}

// Crash_Diagnostic_Preparation is the worker's fixed crash-log prefix or why it was not encoded.
Crash_Diagnostic_Preparation :: union #no_nil {
	Crash_Diagnostic_Fallback,
	Crash_Diagnostic_Not_Encoded,
}

// Route_Matched is the declared method and path template selected before dispatch.
Route_Matched :: struct #all_or_none {
	method: string,
	path: string,
}

// No_Route_Matched identifies a request outside the declared route table.
No_Route_Matched :: struct #all_or_none {}

// Visited_Route preserves whether dispatch selected a declared route.
Visited_Route :: union #no_nil {
	Route_Matched,
	No_Route_Matched,
}

// Received_Method is the request line's method.
Received_Method :: struct #all_or_none {
	value: string,
}

// No_Method is a request whose request line odin-http did not supply.
No_Method :: struct #all_or_none {}

// Request_Method is the method a crash log names, or that the request had none.
Request_Method :: union #no_nil {
	Received_Method,
	No_Method,
}

// Crash_Request_Fields are the request fields of the crash log, shared with Go and Gleam: the
// method, the requested path, and the route template or the path when no route matched.
@(private)
Crash_Request_Fields :: struct #all_or_none {
	event:  string,
	method: Maybe(string),
	path:   Maybe(string),
	route:  Maybe(string),
}

// prepare_crash_fallback encodes the crash-log prefix used when a request's own prefix fails.
@(require_results)
prepare_crash_fallback :: proc() -> Crash_Diagnostic_Preparation {
	switch encoded in encode_crash_prefix(Crash_Request_Fields{event = "request_crashed", method = nil, path = nil, route = nil}) {
	case Encoded_JSON:
		return Crash_Diagnostic_Fallback{prefix = string(encoded.bytes)}
	case JSON_Not_Encoded(Crash_Request_Fields):
		return Crash_Diagnostic_Not_Encoded{failure = encoded.failure}
	}
	unreachable()
}

// protect_request installs the connection and prepared bytes on the dispatching thread.
// The generic input lets a scheduled handler enter the same boundary on its own thread.
@(require_results)
protect_request :: proc(
	request: Request,
	prepared_response: string,
	route: Visited_Route,
	input: $Input,
	answer: proc(Input) -> Answer,
) -> requestcrash.Request_Dispatch(Answer) {
	prefix := prepare_crash_prefix(request, route)
	mode := requestcrash.Request_Protection_Mode(requestcrash.Standalone_Dispatch{})
	switch lifetime in request.lifetime {
	case requestlifetime.No_Request_Deadline:
	case requestlifetime.Deadline_Lifetime:
		mode = requestcrash.Lifetime_Dispatch{lifetime = lifetime.value}
	}
	protection := requestcrash.Request_Protection{socket = request.crash_connection.socket, response = prepared_response, diagnostic_prefix = prefix, mode = mode}
	return requestcrash.answer_request(protection, input, answer)
}

@(private, require_results)
prepare_crash_prefix :: proc(request: Request, route: Visited_Route) -> string {
	fields := Crash_Request_Fields{event = "request_crashed", method = nil, path = request.path, route = request.path}
	switch method in read_request_method(request.content) {
	case Received_Method:
		fields.method = method.value
	case No_Method:
	}
	switch matched in route {
	case Route_Matched:
		fields.route = matched.path
	case No_Route_Matched:
	}
	switch encoded in encode_crash_prefix(fields) {
	case Encoded_JSON:
		return string(encoded.bytes)
	case JSON_Not_Encoded(Crash_Request_Fields):
		return request.crash_connection.fallback.prefix
	}
	unreachable()
}

@(private, require_results)
read_request_method :: proc(content: Request_Content) -> Request_Method {
	switch received in content {
	case Received_Request:
		return Received_Method{value = received.method}
	case Request_Line_Not_Received:
		return No_Method{}
	}
	unreachable()
}

// encode_crash_prefix leaves the object open after its request fields, so the signal handler
// can close it with the cause and stack it alone knows.
@(private, require_results)
encode_crash_prefix :: proc(fields: Crash_Request_Fields) -> JSON_Encoding(Crash_Request_Fields) {
	switch encoded in encode_json(fields) {
	case Encoded_JSON:
		// The encoder owns these bytes and an object always ends in its closing brace.
		encoded.bytes[len(encoded.bytes) - 1] = ','
		return encoded
	case JSON_Not_Encoded(Crash_Request_Fields):
		return encoded
	}
	unreachable()
}
