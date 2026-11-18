package writing

import (
	"fmt"
	"go/format"
	"strconv"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// RenderFilesTest is the formatted test file that fails while any file NoContract generates for the
// project differs from what generate writes now.
func RenderFilesTest(test serverplan.FilesTest) mo.Result[[]byte] {
	file := description.GeneratedHeader + "\npackage " + test.PackageName + "_test\n\n" +
		fmt.Sprintf("import (\n\t\"testing\"\n\n\t%s\n)\n\n", strconv.Quote(serverplan.CheckingPath)) +
		"// TestNocontractFilesCurrent fails when a file nocontract generates differs from what `go tool nocontract generate` writes now.\n" +
		fmt.Sprintf("func TestNocontractFilesCurrent(t *testing.T) {\n\t%s.RequireCurrent(t, %s)\n}\n", serverplan.CheckingPackage, strconv.Quote(test.ProjectFile))
	return mo.TupleToResult(format.Source([]byte(file)))
}
