package nocontract

import "base:runtime"
import "core:encoding/json"
import "core:io"
import "core:strings"

import http "../dependencies/odin-http"
import requestlayers "layers"

// Encoded_JSON is one typed value written as strict JSON.
Encoded_JSON :: struct #all_or_none {
	bytes: []u8,
}

// JSON_Data_Cause is the codec's refusal of a value it cannot represent in JSON.
JSON_Data_Cause :: struct #all_or_none {
	cause: json.Marshal_Data_Error,
}

// JSON_IO_Cause is an output failure reported by the codec.
JSON_IO_Cause :: struct #all_or_none {
	cause: io.Error,
}

// JSON_Allocation_Cause is an allocation failure reported by the codec.
JSON_Allocation_Cause :: struct #all_or_none {
	cause: runtime.Allocator_Error,
}

// JSON_Encode_Cause preserves the kind and original value of a JSON codec failure.
JSON_Encode_Cause :: union #no_nil {
	JSON_Data_Cause,
	JSON_IO_Cause,
	JSON_Allocation_Cause,
}

// JSON_Encode_Failure is the codec operation's partial output, source and original error.
JSON_Encode_Failure :: struct #all_or_none {
	partial:     []u8,
	location:    runtime.Source_Code_Location,
	cause:       JSON_Encode_Cause,
}

// JSON_Not_Encoded preserves the exact typed value and the failed codec operation.
JSON_Not_Encoded :: struct($T: typeid) #all_or_none {
	value: T,
	failure: JSON_Encode_Failure,
}

// JSON_Encoding is the strict JSON output, or the evidence of its codec failure.
JSON_Encoding :: union($T: typeid) #no_nil {
	Encoded_JSON,
	JSON_Not_Encoded(T),
}

// Prepared_Value is a native value converted to the checked wire representation.
Prepared_Value :: struct($T: typeid) #all_or_none {
	value: T,
}

// Native_Literal_Outside keeps an enum value outside its declared wire literals.
Native_Literal_Outside :: struct #all_or_none {
	native_type: typeid,
	read: string,
	declared: []string,
}

// Value_Preparation_Cause keeps each distinct cause of an unwritable native value.
Value_Preparation_Cause :: union #no_nil {
	Instant_Not_Read,
	Native_Literal_Outside,
}

// Value_Not_Prepared keeps the response path and its wire conversion's original cause.
Value_Not_Prepared :: struct #all_or_none {
	path: string,
	cause: Value_Preparation_Cause,
}

// Value_Preparation carries each nested conversion's value or original refusal.
Value_Preparation :: union($T: typeid) #no_nil {
	Prepared_Value(T),
	Value_Not_Prepared,
}

// encode_json writes a generated typed answer without exposing the HTTP response writer.
@(require_results)
encode_json :: proc(value: $T, location := #caller_location) -> JSON_Encoding(T) {
	bytes, failure := json.marshal(value, {spec = .JSON})
	switch cause in failure {
	case nil:
		return Encoded_JSON{bytes = bytes}
	case json.Marshal_Data_Error:
		return JSON_Not_Encoded(T){value = value, failure = JSON_Encode_Failure{partial = bytes, location = location, cause = JSON_Data_Cause{cause = cause}}}
	case io.Error:
		return JSON_Not_Encoded(T){value = value, failure = JSON_Encode_Failure{partial = bytes, location = location, cause = JSON_IO_Cause{cause = cause}}}
	case runtime.Allocator_Error:
		return JSON_Not_Encoded(T){value = value, failure = JSON_Encode_Failure{partial = bytes, location = location, cause = JSON_Allocation_Cause{cause = cause}}}
	}
	unreachable()
}

// JSON_Answer is a generated answer's checked status and JSON.
JSON_Answer :: struct #all_or_none {
	status:  int,
	code:    Maybe(string),
	encoded: Encoded_JSON,
}

@(private, require_results)
describe_answer :: proc(answer: Answer) -> requestlayers.Answered {
	switch body in answer.body {
	case JSON_Answer:
		return requestlayers.Answered{status = body.status, code = body.code}
	case Options_Answer:
		return requestlayers.Answered{status = 204, code = {}}
	}
	unreachable()
}

// Options_Answer is the empty answer to OPTIONS on a declared path.
Options_Answer :: struct #all_or_none {
}

// Answer_Body is JSON or the empty body of an OPTIONS response.
Answer_Body :: union #no_nil {
	JSON_Answer,
	Options_Answer,
}

// Answer is the generated response body and the methods its Allow header names.
Answer :: struct #all_or_none {
	body:    Answer_Body,
	allowed: []string,
}

@(private)
write_answer :: proc(response: ^http.Response, answer: Answer) {
	if len(answer.allowed) > 0 {
		http.headers_set(&response.headers, "Allow", strings.join(answer.allowed, ", "))
	}
	switch sending in answer.body {
	case JSON_Answer:
		write_json_answer(response, sending)
	case Options_Answer:
		http.respond(response, .No_Content)
	}
}

@(private)
write_json_answer :: proc(response: ^http.Response, answer: JSON_Answer) {
	http.headers_set_content_type(&response.headers, "application/json")
	http.response_status(response, http.Status(answer.status))
	http.body_set(response, answer.encoded.bytes)
	http.respond(response)
}
