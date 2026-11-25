package main

import "core:fmt"
import "core:os"
import "core:strconv"

import nocontract "nocontract:odin/runtime"

// with_test_request_deadline applies the deadline SHOP_TEST_REQUEST_DEADLINE names in
// milliseconds, and leaves the shop without a deadline when it is unset.
with_test_request_deadline :: proc(options: nocontract.Listen_Options) -> nocontract.Listen_Options {
	value, set := os.lookup_env(DEADLINE_SETTING, context.temp_allocator)
	if !set {
		return options
	}
	milliseconds, parsed := strconv.parse_u64_of_base(value, 10)
	if !parsed || milliseconds > u64(max(u32)) {
		fmt.eprintln("The shop's request deadline is not a u32 millisecond count:", DEADLINE_SETTING, value)
		os.exit(1)
	}
	switch configured in nocontract.with_request_deadline(options, u32(milliseconds)) {
	case nocontract.Request_Deadline_Outside:
		fmt.eprintln("NoContract deadline refused:", configured)
		os.exit(1)
	case nocontract.Deadline_Configured:
		return configured.options
	}
	unreachable()
}

@(private = "file")
DEADLINE_SETTING :: "SHOP_TEST_REQUEST_DEADLINE"
