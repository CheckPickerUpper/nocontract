package requestlayers

// Visit is the method, actual path, and matched route template or unmatched path.
Visit :: struct #all_or_none {
	method: string,
	path:   string,
	route:  string,
}

// Answered is the status and optional permanent code of the listener's final answer.
Answered :: struct #all_or_none {
	status: int,
	code:   Maybe(string),
}

// Layer observes request entry and the final answer without receiving an HTTP writer.
Layer :: struct #all_or_none {
	user_data: rawptr,
	before:    proc(user_data: rawptr, visit: Visit),
	after:     proc(user_data: rawptr, visit: Visit, answered: Answered),
}

// enter visits each layer in declaration order.
enter :: proc(layers: []Layer, visit: Visit) {
	for layer in layers {
		layer.before(layer.user_data, visit)
	}
}

// leave gives the same final answer to each layer in reverse declaration order.
leave :: proc(layers: []Layer, visit: Visit, answered: Answered) {
	#reverse for layer in layers {
		layer.after(layer.user_data, visit, answered)
	}
}
