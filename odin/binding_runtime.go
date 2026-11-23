package odin

const bindingRuntime = `
@(private)
nc_Answer_Metadata :: struct #all_or_none {
	tag: string,
	status: int,
	code: Maybe(string),
}

@(private, require_results)
nc_encode_answer :: proc(value: $T, metadata: nc_Answer_Metadata) -> nocontract.Answer {
	switch encoded in nocontract.encode_json(value) {
	case nocontract.Encoded_JSON:
		return nocontract.Answer{body = nocontract.JSON_Answer{status = metadata.status, code = metadata.code, encoded = encoded}, allowed = {}}
	case nocontract.JSON_Not_Encoded(T):
		log.errorf("NoContract could not encode %s: %v", metadata.tag, encoded)
		return nc_shared_AnswerNotWritten(nc_fields_AnswerNotWritten{answer = metadata.tag, reason = fmt.tprintf("%v", encoded)}, {})
	}
	unreachable()
}

@(private, require_results)
nc_request_refused :: proc(refusal: nocontract.Request_Refusal) -> nocontract.Answer {
	fields := nocontract.refusal_fields(refusal)
	return nc_shared_RequestRefused(nc_fields_RequestRefused{part = fields.part, name = fields.name, reason = fields.reason}, {})
}

@(private, require_results)
nc_method_refused :: proc(method: string, allowed: []string) -> nocontract.Answer {
	switch method {
	case "OPTIONS":
		return nocontract.Answer{body = nocontract.Options_Answer{}, allowed = allowed}
	}
	return nc_shared_MethodNotDeclared(nc_fields_MethodNotDeclared{method = method, allowed = allowed}, allowed)
}
`
