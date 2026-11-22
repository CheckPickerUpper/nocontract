package running

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/nocontract/internal/projectfile"
)

// NativeBackendFailed is a project whose compiler, generator, or dispatcher could not run.
//
//ergo:code NC-1137
//ergo:message The native backend for {Path} could not run.
type NativeBackendFailed struct {
	Path  string
	Cause error
}

func (NativeBackendFailed) isOutcome() {}

// ExitCode is 2 because the backend could not run.
func (NativeBackendFailed) ExitCode() int { return 2 }

// Lines shows the project path and the compiler, generator, or dispatcher failure.
func (failed NativeBackendFailed) Lines() []string { return described(failed) }

type compiledNative struct {
	checked   assembling.APIAssembled
	execution native.Execution
	files     []generatedFile
}

func generatedNative(context context.Context, project projectfile.Project, finish finisher) Outcome {
	return compileNative(context, project, func(built compiledNative) Outcome {
		failed := func(cause error) Outcome { return FileNotWritten{Path: project.File(), Cause: cause} }
		return outcome.Fold(renderedClientAndOpenAPI(built.checked.API, project), func(public []generatedFile) Outcome {
			return outcome.Fold(plannedGeneratedFiles(project.File(), slices.Concat(public, built.files), nil), func(files []generatedFile) Outcome {
				return onDisk(files, finish)
			}, failed)
		}, failed)
	}, func(failed Outcome) Outcome { return failed })
}

func compileNative[R any](context context.Context, project projectfile.Project, built func(compiledNative) R, refused func(Outcome) R) R {
	request := native.Request{Directory: project.Directory(), Packages: project.Packages(), API: project.API(), Server: project.ServerFolder()}
	fail := func(cause error) R { return refused(NativeBackendFailed{Path: project.File(), Cause: cause}) }
	return outcome.Fold(readNative(context, project.Backend(), request), func(back native.Project) R {
		switch assembled := assembling.Assemble(back.Declared).(type) {
		case assembling.APIAssembled:
			return outcome.Fold(back.Generate(assembled), func(nativeFiles []native.File) R {
				return outcome.Fold(renderNativeFiles(context, project.File(), nativeFiles), func(artifacts []generatedFile) R {
					return built(compiledNative{checked: assembled, execution: back.Execution, files: artifacts})
				}, fail)
			}, fail)
		case assembling.APIRefused:
			return refused(DeclarationsRefused{Refusals: assembled.Refusals})
		case nil:
		}
		return fail(nativeContractMissing{Directory: project.Directory()})
	}, fail)
}

func renderNativeFiles(context context.Context, project string, artifacts []native.File) outcome.Result[[]generatedFile] {
	files := make([]generatedFile, 0, len(artifacts))
	for _, artifact := range artifacts {
		firstBytes, _, _ := bytes.Cut(artifact.Contents, []byte("\n"))
		first := string(firstBytes)
		if strings.TrimLeft(first, "/#% ") != description.GeneratedNotice {
			return outcome.Err[[]generatedFile](nativeSourceHeaderMissing{Path: artifact.Path, FirstLine: first})
		}
		files = append(files, generatedFile{path: filepath.Clean(artifact.Path), content: outcome.Some(nativeSourceOutput(context, artifact)), owner: headedOutput{header: first + "\n", source: "generated source"}})
	}
	return plannedGeneratedFiles(project, files, nil)
}
