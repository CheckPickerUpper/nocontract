package nocontract

@(private)
No_Deadline :: struct #all_or_none {}

@(private)
Configured_Deadline :: struct #all_or_none {
	milliseconds: u32,
}

@(private)
Request_Deadline :: union #no_nil {
	No_Deadline,
	Configured_Deadline,
}

// Deadline_Configured is a listener with a positive, bounded request deadline in whole milliseconds.
Deadline_Configured :: struct #all_or_none {
	options: Listen_Options,
}

// Request_Deadline_Outside keeps the attempted duration and the accepted bounds.
@(error_code = "NC-1171")
Request_Deadline_Outside :: struct #all_or_none {
	timeout_milliseconds: u32,
	least: u32,
	most: u32,
}

// Deadline_Configuration is the configured listener or its invalid requested duration.
Deadline_Configuration :: union #no_nil {
	Deadline_Configured,
	Request_Deadline_Outside,
}

// with_request_deadline chooses the positive whole milliseconds available in every server language.
@(require_results)
with_request_deadline :: proc(options: Listen_Options, milliseconds: u32) -> Deadline_Configuration {
	if milliseconds == 0 {
		return Request_Deadline_Outside{timeout_milliseconds = milliseconds, least = 1, most = 4_294_967_295}
	}
	return Deadline_Configured{options = Listen_Options{tcp = TCP_Listen_Options{endpoint = options.tcp.endpoint, ready = options.tcp.ready, layers = options.tcp.layers, deadline = Configured_Deadline{milliseconds = milliseconds}}}}
}
