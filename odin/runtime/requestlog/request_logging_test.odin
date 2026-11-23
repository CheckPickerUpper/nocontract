#+test
package requestlog

import "core:fmt"
import "core:log"
import "core:os"
import "core:strings"
import "core:testing"
import "core:time"

@(test)
finalized_request_logs_reach_the_application_logger :: proc(test: ^testing.T) {
	file, create_error := os.create_temp_file(os.get_env("TEST_SCRATCH", context.temp_allocator), "request-log-*")
	if !testing.expect_value(test, create_error, os.Error{}) {
		return
	}
	path := strings.clone(os.name(file), context.temp_allocator)
	defer testing.expect_value(test, os.remove(path), os.Error{})
	context.logger = log.create_file_logger(file, opt = {})
	defer log.destroy_file_logger(context.logger)

	entries := []Request_Log{
		{
			route = "/shoppers/{shopper_id}", method = "GET", status = 200,
			code = {}, duration = 17 * time.Microsecond + 999 * time.Nanosecond,
		},
		{
			route = "/shoppers/{shopper_id}", method = "GET", status = 404,
			code = "SH-1001", duration = 2301 * time.Microsecond,
		},
		{
			route = "/missing\n\"path", method = "ODD\rMETHOD", status = 404,
			code = "NC-1069", duration = 8 * time.Microsecond,
		},
	}
	for entry in entries {
		switch emitted in log_request(entry) {
		case Request_Logged:
		case Request_Log_Not_Encoded:
			testing.fail_now(test, fmt.tprintf("request log %v could not be encoded: %v", emitted.request, emitted.cause))
		}
	}
	_, seek_error := os.seek(file, 0, .Start)
	if !testing.expect_value(test, seek_error, os.Error{}) {
		return
	}
	output, read_error := os.read_entire_file(file, context.allocator)
	if !testing.expect_value(test, read_error, os.Error{}) {
		return
	}
	defer delete(output)

	expected := "{\"duration_us\":17,\"method\":\"GET\",\"route\":\"/shoppers/{shopper_id}\",\"status\":200}\n" +
	            "{\"code\":\"SH-1001\",\"duration_us\":2301,\"method\":\"GET\",\"route\":\"/shoppers/{shopper_id}\",\"status\":404}\n" +
	            "{\"code\":\"NC-1069\",\"duration_us\":8,\"method\":\"ODD\\rMETHOD\",\"route\":\"/missing\\n\\\"path\",\"status\":404}\n"
	testing.expect_value(test, string(output), expected)
}
