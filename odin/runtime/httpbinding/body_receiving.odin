package httpbinding

import "core:bufio"
import "core:io"

import http "../../dependencies/odin-http"

// Received_Body is the body odin-http read for one request.
Received_Body :: struct #all_or_none {
}

// Body_Scan_Cause is odin-http's body framing failure.
Body_Scan_Cause :: struct #all_or_none {
	cause: bufio.Scanner_Extra_Error,
}

// Body_IO_Cause is the original socket or reader failure.
Body_IO_Cause :: struct #all_or_none {
	cause: io.Error,
}

// Body_Read_Cause preserves the kind and original value of a body read failure.
Body_Read_Cause :: union #no_nil {
	Body_Scan_Cause,
	Body_IO_Cause,
}

// Body_Not_Received keeps the request and every byte odin-http returned with its read failure.
Body_Not_Received :: struct #all_or_none {
	path:  string,
	cause: Body_Read_Cause,
}

// Body_Size_Exceeded retains odin-http's exact limit, consumed length, next length and framing.
Body_Size_Exceeded :: struct #all_or_none {
	cause: http.Body_Size_Error,
}

// Body_Reading preserves whether the body completed or its transport failed.
Body_Reading :: union #no_nil {
	Received_Body,
	Body_Not_Received,
	Body_Size_Exceeded,
}

// Body_Receipt keeps every byte odin-http read beside its distinct completion or failure.
Body_Receipt :: struct #all_or_none {
	text:    string,
	reading: Body_Reading,
}

@(private)
Body_Callback_State :: struct($T: typeid) #all_or_none {
	receiver: ^T,
	receive:  proc(receiver: ^T, receipt: Body_Receipt),
	path:     string,
}

// receive_body binds odin-http's callback to typed request state. The request's temporary
// allocator owns that state until the response's last callback releases the request.
receive_body :: proc(
	request: ^http.Request,
	max_length: int,
	receiver: ^$T,
	receive: proc(receiver: ^T, receipt: Body_Receipt),
) {
	state := new_clone(
		Body_Callback_State(T){receiver = receiver, receive = receive, path = request.url.path},
		context.temp_allocator,
	)
	http.body(request, max_length, state, proc(pointer: rawptr, body: http.Body, failure: http.Body_Error) {
		state := cast(^Body_Callback_State(T))pointer
		switch cause in failure {
		case nil:
			state.receive(state.receiver, Body_Receipt{text = body, reading = Received_Body{}})
		case bufio.Scanner_Extra_Error:
			state.receive(state.receiver, Body_Receipt{text = body, reading = Body_Not_Received{path = state.path, cause = Body_Scan_Cause{cause = cause}}})
		case io.Error:
			state.receive(state.receiver, Body_Receipt{text = body, reading = Body_Not_Received{path = state.path, cause = Body_IO_Cause{cause = cause}}})
		case http.Body_Size_Error:
			state.receive(state.receiver, Body_Receipt{text = body, reading = Body_Size_Exceeded{cause = cause}})
		}
	})
}
