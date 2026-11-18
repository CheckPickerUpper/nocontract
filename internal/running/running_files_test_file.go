package running

import (
	"cmp"
	"path/filepath"
	"slices"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/go/writing"
)

// projectPaths is where a project's own files are, each absolute: its project file, the TypeScript
// file it names, and the OpenAPI file it names with the version that file declares.
type projectPaths struct {
	projectFile string
	typeScript  string
	openAPI     OpenAPIFile
}

// servedFiles is each package's generated Go file, then the test that fails while any generated
// file is stale, or why one could not be rendered.
func servedFiles(served []serverplan.Package, projectFile string) mo.Result[[]generatedFile] {
	return mo.Fold(goFiles(served), func(goFile []generatedFile) mo.Result[[]generatedFile] {
		return mo.Fold(filesTestFiles(served, projectFile), func(tests []generatedFile) mo.Result[[]generatedFile] {
			return mo.Ok(append(goFile, tests...))
		}, mo.Err[[]generatedFile])
	}, mo.Err[[]generatedFile])
}

// filesTestFiles is the test written into the package whose folder sorts first among those given a
// generated Go file, or none when no package is given one.
func filesTestFiles(served []serverplan.Package, projectFile string) mo.Result[[]generatedFile] {
	if len(served) == 0 {
		return mo.Ok([]generatedFile{})
	}
	first := slices.MinFunc(served, func(one serverplan.Package, other serverplan.Package) int {
		return cmp.Compare(one.Directory, other.Directory)
	})
	return mo.Fold(mo.TupleToResult(filepath.Rel(first.Directory, projectFile)), func(relative string) mo.Result[[]generatedFile] {
		test := serverplan.FilesTest{PackageName: first.Name, ProjectFile: filepath.ToSlash(relative)}
		return mo.Fold(writing.RenderFilesTest(test), func(content []byte) mo.Result[[]generatedFile] {
			return mo.Ok([]generatedFile{{path: filepath.Join(first.Directory, serverplan.FilesTestFileName), content: mo.Some(content)}})
		}, mo.Err[[]generatedFile])
	}, mo.Err[[]generatedFile])
}
