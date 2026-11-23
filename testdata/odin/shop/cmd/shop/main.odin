package main

import "core:fmt"
import "core:net"
import "core:os"
import "core:log"

import nocontract "nocontract:odin/runtime"
import requestlayers "nocontract:odin/runtime/layers"
import server "../../server"

main :: proc() {
	context.logger = log.create_console_logger(.Info)
	defer log.destroy_console_logger(context.logger)
	outer, inner := "outer", "inner"
	options := nocontract.notify_when_ready(
		nocontract.listen_on(net.Endpoint{address = net.IP4_Loopback, port = 0}),
		proc(endpoint: net.Endpoint) {
			fmt.printf("Listening on http://%s\n", net.endpoint_to_string(endpoint))
		},
	)
	switch parsed in nocontract.parse_body_limit(1024) {
	case nocontract.Request_Body_Limit:
		options = nocontract.with_body_limit(options, parsed)
	case nocontract.Invalid_Request_Body_Limit:
		fmt.eprintln("NoContract body limit refused:", parsed)
		os.exit(1)
	}
	options = nocontract.with_layers(options, []requestlayers.Layer{
		{user_data = &outer, before = observe_request_entry, after = observe_request_answer},
		{user_data = &inner, before = observe_request_entry, after = observe_request_answer},
	})
	if os.get_env("SHOP_TEST_REQUEST_DEADLINE", context.temp_allocator) == "1" {
		switch configured in nocontract.with_request_deadline(options, 50) {
		case nocontract.Request_Deadline_Outside:
			fmt.eprintln("NoContract deadline refused:", configured)
			os.exit(1)
		case nocontract.Deadline_Configured:
			options = configured.options
		}
	}
	switch terminal in server.serve(options) {
	case nocontract.Server_Stopped:
		return
	case nocontract.Listener_Event_Loop_Not_Available, nocontract.Listener_Not_Opened,
		nocontract.Listener_Endpoint_Not_Read, nocontract.Listener_Not_Served:
		fmt.eprintln("NoContract server failed:", terminal)
		os.exit(1)
	}
}
