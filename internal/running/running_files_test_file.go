package running

import (
	"cmp"
	"path/filepath"
	"slices"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/go/writing"
)

// servedFiles is each package's generated Go file, the server file, then the test that fails while
// any generated file is stale, or why one could not be rendered.
func servedFiles(api description.API, plan serverplan.Plan, projectFile string) outcome.Result[[]generatedFile] {
	return outcome.Fold(goFiles(plan.Packages), func(goFile []generatedFile) outcome.Result[[]generatedFile] {
		return outcome.Fold(writing.RenderServer(plan, api), func(server []byte) outcome.Result[[]generatedFile] {
			serverFile := generatedFile{path: filepath.Join(plan.Server.Directory, serverplan.ServerFileName), content: mo.Some(server)}
			return outcome.Fold(filesTestFiles(plan.Packages, projectFile), func(tests []generatedFile) outcome.Result[[]generatedFile] {
				return outcome.Ok(slices.Concat(goFile, []generatedFile{serverFile}, tests))
			}, outcome.Err[[]generatedFile])
		}, outcome.Err[[]generatedFile])
	}, outcome.Err[[]generatedFile])
}

// filesTestFiles is the test written into the package whose folder sorts first among those given a
// generated Go file, or none when no package is given one.
func filesTestFiles(served []serverplan.Package, projectFile string) outcome.Result[[]generatedFile] {
	if len(served) == 0 {
		return outcome.Ok([]generatedFile{})
	}
	first := slices.MinFunc(served, func(one serverplan.Package, other serverplan.Package) int {
		return cmp.Compare(one.Directory, other.Directory)
	})
	return outcome.Fold(outcome.TupleToResult(filepath.Rel(first.Directory, projectFile)), func(relative string) outcome.Result[[]generatedFile] {
		test := serverplan.FilesTest{PackageName: first.Name, ProjectFile: filepath.ToSlash(relative)}
		return outcome.Fold(writing.RenderFilesTest(test), func(content []byte) outcome.Result[[]generatedFile] {
			return outcome.Ok([]generatedFile{{path: filepath.Join(first.Directory, serverplan.FilesTestFileName), content: mo.Some(content)}})
		}, outcome.Err[[]generatedFile])
	}, outcome.Err[[]generatedFile])
}
