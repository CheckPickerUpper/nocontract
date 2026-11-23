package odin

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/outcome"
)

// generateFilesTest emits the check run from the application's project directory by odin test.
func generateFilesTest(request native.Request) outcome.Result[native.File] {
	return outcome.Fold(outcome.TupleToResult(filepath.Rel(request.Directory, request.API.Position.File)), func(relative string) outcome.Result[native.File] {
		source := description.GeneratedHeader + fmt.Sprintf(`
#+test
package nocontract_server

import filecheck "nocontract:odin/runtime/filecheck"
import "core:fmt"
import "core:testing"

@(test)
nocontract_files_are_current :: proc(t: ^testing.T) {
	switch checked in filecheck.check(%s, context.temp_allocator) {
	case filecheck.Current_Files:
	case filecheck.Command_Exited:
		nocontract_report_failed_execution(t, fmt.tprintf("%%T", checked), checked.execution)
	case filecheck.Command_Not_Completed:
		nocontract_report_failed_execution(t, fmt.tprintf("%%T: %%v", checked, checked.cause), checked.execution)
	case filecheck.Arguments_Not_Copied, filecheck.Directory_Unreadable, filecheck.Environment_Unreadable:
		testing.expect(t, false, fmt.tprintf("%%v", checked))
	}
}

@(private="file")
nocontract_report_failed_execution :: proc(t: ^testing.T, refusal: string, execution: filecheck.Check_Execution) {
	testing.expect(t, false, fmt.tprintf("%%s\ncommand: %%v\nprocess: %%v\nelapsed: %%v\nstdout:\n%%s\nstderr:\n%%s", refusal, execution.command, execution.process, execution.elapsed, string(execution.stdout), string(execution.stderr)))
}
`, strconv.Quote(filepath.ToSlash(relative)))
		return outcome.Ok(native.File{Path: filepath.Join(request.Server, "nocontract_files_test.odin"), Contents: []byte(source)})
	}, outcome.Err[native.File])
}
