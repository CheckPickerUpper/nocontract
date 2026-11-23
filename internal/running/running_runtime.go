package running

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/go/writing"
	"github.com/CheckPickerUpper/nocontract/messages"
)

// RunRuntime runs generate or check on nocontract's own runtime package, found from the current
// folder's module: its generated file answers each failure the runtime answers on a route's
// behalf with the status that failure's directive declares.
func RunRuntime(request Request) Outcome {
	switch parsed := parseVerb(request.Arguments, flag.NewFlagSet("runtime-answers", flag.ContinueOnError)).(type) {
	case verbRead:
		return mo.Fold(runtimeDirectory(request.Context), func(directory string) Outcome {
			return generatedRuntime(directory, parsed.finish)
		}, func(cause error) Outcome { return PackagesNotLoaded{Errors: []error{cause}} })
	case verbRefused:
		return UsageRefused{Problem: parsed.problem, Usage: runtimeUsage}
	case nil:
	}
	return UsageRefused{Problem: NoCommand{}, Usage: runtimeUsage}
}

// runtimeUsage is how the runtime-answers command is run.
const runtimeUsage = "runtime-answers generate|check"

// RuntimeNotFound is a module whose packages do not include nocontract's runtime package.
//
//ergo:code NC-1058
//ergo:message The current folder's module does not hold NoContract's runtime package.
type RuntimeNotFound struct{}

// Error is the failure's message in the source language.
func (missing RuntimeNotFound) Error() string { return missing.Report(messages.Match("")).Message }

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

// generatedRuntime renders the runtime package's generated file from the one declaration of
// nocontract's own failures, and hands it, with what its path holds, to finish. Nothing of the
// runtime is read, so a runtime whose generated file is missing or stale is written again.
func generatedRuntime(directory string, finish finisher) Outcome {
	path := filepath.Join(directory, serverplan.RoutesFileName)
	return outcome.Fold(writing.RenderRuntime(description.SharedFailures()), func(content []byte) Outcome {
		return onDisk([]generatedFile{goFileAt(path, content)}, finish)
	}, func(cause error) Outcome { return FileNotWritten{Path: path, Cause: cause} })
}
