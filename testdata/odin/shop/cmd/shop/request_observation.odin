package main

import "core:log"

import nocontract "nocontract:odin/runtime"
import requestlayers "nocontract:odin/runtime/layers"

@(private)
observe_request_entry :: proc(user_data: rawptr, visit: requestlayers.Visit) {
	name := cast(^string)user_data
	value := struct #all_or_none {
		layer: string,
		phase: string,
		method: string,
		path: string,
		route: string,
	}{layer = name^, phase = "before", method = visit.method, path = visit.path, route = visit.route}
	emit_request_observation(value)
}

@(private)
observe_request_answer :: proc(user_data: rawptr, visit: requestlayers.Visit, answered: requestlayers.Answered) {
	name := cast(^string)user_data
	value := struct #all_or_none {
		layer: string,
		phase: string,
		method: string,
		path: string,
		route: string,
		status: int,
		code: Maybe(string) `json:"code,omitempty"`,
	}{layer = name^, phase = "after", method = visit.method, path = visit.path, route = visit.route, status = answered.status, code = answered.code}
	emit_request_observation(value)
}

@(private)
emit_request_observation :: proc(value: $T) {
	switch encoded in nocontract.encode_json(value) {
	case nocontract.Encoded_JSON:
		defer delete(encoded.bytes)
		log.info(string(encoded.bytes))
	case nocontract.JSON_Not_Encoded(T):
		log.errorf("The shop could not encode its layer observation: %v", encoded)
	}
}
