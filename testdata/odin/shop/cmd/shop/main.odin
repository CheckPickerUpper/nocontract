package main

import "core:fmt"
import "core:net"
import "core:os"
import "core:log"
import "core:strconv"
import "core:sys/linux"
import "core:sys/posix"
import "core:time"

import nocontract "nocontract:odin/runtime"
import requestlayers "nocontract:odin/runtime/layers"
import server "../../server"

main :: proc() {
	// Linux's piped core collector ignores RLIMIT_CORE for these deliberate fixture traps.
	PR_SET_DUMPABLE :: 4
	if cause := linux.prctl(PR_SET_DUMPABLE, 0, 0, 0, 0); cause != .NONE {
		fmt.eprintln("The shop could not disable deliberate crash core collection:", cause)
		os.exit(1)
	}
	// The supervisor hands its worker the changed value, so only the worker crashes, before it
	// reports ready.
	startup_crash := os.get_env("SHOP_TEST_STARTUP_CRASH", context.temp_allocator)
	if startup_crash == "supervisor" {
		if cause := os.set_env("SHOP_TEST_STARTUP_CRASH", "worker"); cause != nil {
			fmt.eprintln("The shop could not mark its worker's startup crash:", cause)
			os.exit(1)
		}
	} else if startup_crash == "worker" {
		_ = posix.raise(.SIGSEGV)
	}
	context.logger = log.create_console_logger(.Info)
	defer log.destroy_console_logger(context.logger)
	outer, inner := "outer", "inner"
	port := 0
	if requested, found := os.lookup_env("SHOP_TEST_LISTEN_PORT", context.temp_allocator); found {
		parsed, read := strconv.parse_int(requested, 10)
		if !read {
			fmt.eprintln("The shop could not read its test listen port:", requested)
			os.exit(1)
		}
		port = parsed
	}
	options := nocontract.notify_when_ready(
		nocontract.listen_on(net.Endpoint{address = net.IP4_Loopback, port = port}),
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
	if os.get_env("SHOP_TEST_SHORT_SHUTDOWN", context.temp_allocator) == "1" {
		options = nocontract.with_shutdown_period(options, 50 * time.Millisecond)
	}
	options = with_test_request_deadline(options)
	switch terminal in server.serve(options) {
	case nocontract.Server_Stopped:
		return
	case nocontract.Listener_Not_Opened, nocontract.Listener_Not_Served, nocontract.Listener_Not_Stopped:
		fmt.eprintln("NoContract server failed:", terminal)
		os.exit(1)
	}
}
