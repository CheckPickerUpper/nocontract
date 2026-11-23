package requestlog

import "core:encoding/json"
import "core:log"
import "core:time"

// Request_Log records a finalized answer. Route is the declared path template, or the
// actual path when unmatched. Duration is elapsed time measured by the server's monotonic clock.
Request_Log :: struct #all_or_none {
	route:    string,
	method:   string,
	status:   int,
	code:     Maybe(string),
	duration: time.Duration,
}

// Request_Logged is a request event submitted to the application's logger.
@(not_an_error)
Request_Logged :: struct #all_or_none {}

// Request_Log_Not_Encoded keeps the request event and the native JSON encoder's cause.
Request_Log_Not_Encoded :: struct #all_or_none {
	request: Request_Log,
	cause:   json.Marshal_Error,
}

// Request_Log_Emission is whether the native JSON encoder supplied the logger's message.
Request_Log_Emission :: union #no_nil {
	Request_Logged,
	Request_Log_Not_Encoded,
}

@(private)
Request_Log_Message :: struct #all_or_none {
	code:        Maybe(string) `json:"code,omitempty"`,
	duration_us: i64,
	method:      string,
	route:       string,
	status:      int,
}

// log_request emits one JSON request event through the application's core logger.
@(require_results)
log_request :: proc(entry: Request_Log) -> Request_Log_Emission {
	message := Request_Log_Message{
		code = entry.code,
		duration_us = i64(entry.duration / time.Microsecond),
		method = entry.method,
		route = entry.route,
		status = entry.status,
	}
	encoded, cause := json.marshal(message)
	defer delete(encoded)
	if cause != nil {
		return Request_Log_Not_Encoded{request = entry, cause = cause}
	}
	log.info(string(encoded))
	return Request_Logged{}
}
