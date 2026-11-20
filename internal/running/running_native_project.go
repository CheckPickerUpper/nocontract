package running

import (
	"bytes"
	"cmp"
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"

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
	checked assembling.APIAssembled
	project native.Project
	files   []generatedFile
}

func generatedNative(context context.Context, project projectfile.Project, finish finisher) Outcome {
	return compileNative(context, project, func(built compiledNative) Outcome {
		return onDisk(built.files, finish)
	}, func(failed Outcome) Outcome { return failed })
}

func compileNative[R any](context context.Context, project projectfile.Project, built func(compiledNative) R, refused func(Outcome) R) R {
	request := native.Request{Directory: project.Directory(), Packages: project.Packages(), API: project.API(), Server: project.ServerFolder()}
	fail := func(cause error) R { return refused(NativeBackendFailed{Path: project.File(), Cause: cause}) }
	return outcome.Fold(readNative(context, project.Backend(), request), func(back native.Project) R {
		switch assembled := assembling.Assemble(back.Declared).(type) {
		case assembling.APIAssembled:
			return outcome.Fold(back.Generate(assembled), func(nativeFiles []native.File) R {
				return outcome.Fold(renderNativeFiles(assembled.API, project, nativeFiles), func(artifacts []generatedFile) R {
					return built(compiledNative{checked: assembled, project: back, files: artifacts})
				}, fail)
			}, fail)
		case assembling.APIRefused:
			return refused(DeclarationsRefused{Refusals: assembled.Refusals})
		case nil:
		}
		return fail(nativeContractMissing{Directory: project.Directory()})
	}, fail)
}

func renderNativeFiles(api description.API, project projectfile.Project, artifacts []native.File) outcome.Result[[]generatedFile] {
	return outcome.Fold(renderedClients(api, project), func(files []generatedFile) outcome.Result[[]generatedFile] {
		for _, artifact := range artifacts {
			firstBytes, _, _ := bytes.Cut(artifact.Contents, []byte("\n"))
			first := string(firstBytes)
			marker := strings.TrimPrefix(description.GeneratedHeader, "// ")
			if strings.TrimLeft(first, "/#% ")+"\n" != marker {
				return outcome.Err[[]generatedFile](nativeSourceHeaderMissing{Path: artifact.Path, FirstLine: first})
			}
			files = append(files, generatedFile{path: filepath.Clean(artifact.Path), content: mo.Some(artifact.Contents), owner: headedOutput{header: first + "\n"}})
		}
		slices.SortFunc(files, func(first, second generatedFile) int { return cmp.Compare(first.path, second.path) })
		for index, file := range files {
			if index > 0 && files[index-1].path == file.path {
				return outcome.Err[[]generatedFile](generatedPathsOverlap{First: files[index-1].path, Second: file.path})
			}
		}
		return outcome.Ok(files)
	}, outcome.Err[[]generatedFile])
}
