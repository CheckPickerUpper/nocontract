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

// NativeBackendFailed is a project whose compiler or generator could not run.
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

// Lines shows the project path and the compiler or generator failure.
func (failed NativeBackendFailed) Lines() []string { return described(failed) }

type compiledNative struct {
	checked assembling.APIAssembled
	files   []generatedFile
}

type nativeReader func(context.Context, native.Request) outcome.Result[native.Project]

func generatedNative(ctx context.Context, project projectfile.Project, read nativeReader, finish finisher) Outcome {
	return compileNative(ctx, project, read, func(built compiledNative) Outcome {
		failed := func(cause error) Outcome { return FileNotWritten{Path: project.File(), Cause: cause} }
		return outcome.Fold(renderedClientAndOpenAPI(built.checked.API, project), func(public []generatedFile) Outcome {
			return outcome.Fold(plannedGeneratedFiles(project.File(), slices.Concat(public, built.files), nil), func(files []generatedFile) Outcome {
				return onDisk(files, finish)
			}, failed)
		}, failed)
	})
}

func compileNative(ctx context.Context, project projectfile.Project, read nativeReader, built func(compiledNative) Outcome) Outcome {
	collections := []native.OdinCollection{}
	switch backend := project.Backend().(type) {
	case projectfile.OdinBackend:
		collections = backend.Collections()
	case projectfile.GoBackend:
	case projectfile.GleamBackend:
	case nil:
	}
	request := native.Request{Directory: project.Directory(), Packages: project.Packages(), API: project.API(), Server: project.ServerFolder(), OdinCollections: collections}
	fail := func(cause error) Outcome { return NativeBackendFailed{Path: project.File(), Cause: cause} }
	return outcome.Fold(read(ctx, request), func(back native.Project) Outcome {
		switch assembled := assembling.Assemble(back.Declared).(type) {
		case assembling.APIAssembled:
			return outcome.Fold(back.Generate(assembled), func(nativeFiles []native.File) Outcome {
				return outcome.Fold(renderNativeFiles(ctx, project.File(), nativeFiles), func(artifacts []generatedFile) Outcome {
					return built(compiledNative{checked: assembled, files: artifacts})
				}, fail)
			}, fail)
		case assembling.APIRefused:
			return DeclarationsRefused{Refusals: assembled.Refusals}
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
