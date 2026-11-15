package running

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/go/reading"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// RunRuntime runs generate or check on nocontract's own runtime package, found from the current
// folder's module: its generated file answers each failure the runtime answers on a route's
// behalf with the status that failure's directive declares.
func RunRuntime(request Request) Outcome {
	switch parsed := parseVerb(request.Arguments, flag.NewFlagSet("runtime-answers", flag.ContinueOnError)).(type) {
	case verbRead:
		return mo.Fold(runtimeDirectory(request.Context), func(directory string) Outcome {
			return generatedRuntime(request.Context, directory, parsed.finish)
		}, func(cause error) Outcome { return PackagesNotLoaded{Errors: []error{cause}} })
	case UsageRefused:
		return parsed
	case nil:
	}
	return UsageRefused{Problem: NoCommand{}}
}

// RuntimeNotFound is a module whose packages do not include nocontract's runtime package.
//
//ergo:code NC-1058
type RuntimeNotFound struct{}

func (RuntimeNotFound) Error() string {
	return "the current folder's module does not hold " + serverplan.NoContractPath
}

// runtimeDirectory is the folder holding the runtime package's files.
func runtimeDirectory(ctx context.Context) mo.Result[string] {
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles, Context: ctx, Logf: func(string, ...any) {}, Dir: ".", Env: os.Environ(),
		BuildFlags: []string{}, Fset: nil, ParseFile: nil, Tests: false, Overlay: map[string][]byte{},
	}
	return mo.Fold(mo.TupleToResult(packages.Load(config, serverplan.NoContractPath)), func(loaded []*packages.Package) mo.Result[string] {
		for _, each := range loaded {
			if each.PkgPath == serverplan.NoContractPath && len(each.GoFiles) > 0 {
				return mo.Ok(filepath.Dir(each.GoFiles[0]))
			}
		}
		return mo.Err[string](RuntimeNotFound{})
	}, mo.Err[string])
}

// generatedRuntime reads the runtime package's shared failures, renders its generated file and
// hands it to finish.
func generatedRuntime(ctx context.Context, directory string, finish finisher) Outcome {
	path := filepath.Join(directory, serverplan.FileName)
	existing := mo.Fold(mo.TupleToResult(os.ReadFile(path)), func(content []byte) map[string][]byte { return map[string][]byte{path: content} },
		// A file that cannot be read is written again; nothing of it is read as a stub.
		func(error) map[string][]byte { return map[string][]byte{} })
	return mo.Fold(loadPackages(ctx, directory, []string{}, existing), func(loaded []*packages.Package) Outcome {
		if problems := loadProblems(loaded, []string{serverplan.NoContractPath}); len(problems) > 0 {
			return PackagesNotLoaded{Errors: problems}
		}
		switch read := reading.ReadRuntime(loaded).(type) {
		case reading.RuntimeRead:
			return mo.Fold(goFiles([]serverplan.Package{read.Package}), finish, func(cause error) Outcome { return FileNotWritten{Path: path, Cause: cause} })
		case reading.RuntimeRefused:
			return DeclarationsRefused{Refusals: read.Refusals}
		case nil:
		}
		return DeclarationsRefused{Refusals: []reading.Refusal{}}
	}, func(cause error) Outcome { return PackagesNotLoaded{Errors: []error{cause}} })
}
