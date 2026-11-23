package bindings

import (
	"fmt"
	"path/filepath"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/outcome"
)

// GenerateFilesTest emits the gleeunit-discovered check into the application's test folder.
func GenerateFilesTest(request native.Request) outcome.Result[native.File] {
	return outcome.Fold(outcome.TupleToResult(filepath.Rel(request.Directory, request.API.Position.File)), func(relative string) outcome.Result[native.File] {
		source := description.GeneratedHeader + "import nocontract_runtime/generated_files\n\n" +
			fmt.Sprintf(`pub fn generated_files_are_current_test() -> Nil {
  let checked = generated_files.check(%s)
  let assert Ok(_) = checked as case checked {
    Ok(_) -> ""
    Error(refusal) -> generated_files.describe(refusal)
  }
  Nil
}
`, quoted(filepath.ToSlash(relative)))
		return outcome.Ok(native.File{Path: filepath.Join(request.Directory, "test", "nocontract_files_test.gleam"), Contents: []byte(source)})
	}, outcome.Err[native.File])
}
