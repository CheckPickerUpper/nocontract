package gleam

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/gleam/directives"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/nocontract/internal/sequence"
	"github.com/CheckPickerUpper/outcome"
)

// CompilerViewCleanupFailed is a compiler view that could not be removed after its read.
//
//ergo:code NC-1147
//ergo:message The Gleam compiler view {Path} could not be removed.
type CompilerViewCleanupFailed struct {
	Directory string
	Path      string
	Cause     error
}

func (failure CompilerViewCleanupFailed) Error() string {
	return fmt.Sprintf("Gleam compiler source view %s in %s could not be removed: %v", failure.Path, failure.Directory, failure.Cause)
}
func (failure CompilerViewCleanupFailed) Unwrap() error { return failure.Cause }

// CompilerSourceLinkFailed is a source or build link the compiler view could not create.
//
//ergo:code NC-1148
//ergo:message The Gleam compiler link {Link} to {Source} could not be created.
type CompilerSourceLinkFailed struct {
	Source string
	Link   string
	Cause  error
}

func (failure CompilerSourceLinkFailed) Error() string {
	return fmt.Sprintf("Gleam compiler link %s to %s: %v", failure.Link, failure.Source, failure.Cause)
}
func (failure CompilerSourceLinkFailed) Unwrap() error { return failure.Cause }

func readSelectedProject(context context.Context, request native.Request) outcome.Result[compiler.Package] {
	return outcome.Fold(directives.Sources(request.Directory), func(modules map[string]directives.SourceModule) outcome.Result[compiler.Package] {
		selected := selectedSources(request.Packages, modules)
		outputs := compilerOutputDirectory(request.Directory)
		return outcome.FoldOption(outcome.EmptyableToOption(os.MkdirAll(outputs, 0o755)), outcome.Err[compiler.Package], func() outcome.Result[compiler.Package] {
			return outcome.Fold(outcome.TupleToResult(os.MkdirTemp(outputs, "view-")), func(view string) outcome.Result[compiler.Package] {
				read := compileSourceView(context, request.Directory, view, selected)
				return outcome.FoldOption(outcome.EmptyableToOption(os.RemoveAll(view)), func(cause error) outcome.Result[compiler.Package] {
					return outcome.Fold(read, func(compiler.Package) outcome.Result[compiler.Package] {
						return outcome.Err[compiler.Package](CompilerViewCleanupFailed{Directory: request.Directory, Path: view, Cause: cause})
					}, func(readError error) outcome.Result[compiler.Package] {
						return outcome.Err[compiler.Package](errors.Join(readError, CompilerViewCleanupFailed{Directory: request.Directory, Path: view, Cause: cause}))
					})
				}, func() outcome.Result[compiler.Package] { return read })
			}, outcome.Err[compiler.Package])
		})
	}, outcome.Err[compiler.Package])
}

func compilerOutputDirectory(directory string) string {
	return filepath.Join(directory, "build", "nocontract-compiler")
}

func selectedSources(patterns []string, modules map[string]directives.SourceModule) []directives.SourceModule {
	roots := []string{}
	for _, pattern := range patterns {
		if pattern != "./..." {
			roots = append(roots, pattern)
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(modules)) {
			if len(modules[name].Directives) > 0 {
				roots = append(roots, name)
			}
		}
	}
	seen := map[string]struct{}{}
	selected := []directives.SourceModule{}
	for len(roots) > 0 {
		name := roots[0]
		roots = roots[1:]
		if _, visited := seen[name]; visited {
			continue
		}
		seen[name] = struct{}{}
		if module, found := modules[name]; found {
			selected = append(selected, module)
			roots = append(roots, module.Imports...)
		}
	}
	return selected
}

func compileSourceView(context context.Context, directory, view string, sources []directives.SourceModule) outcome.Result[compiler.Package] {
	return outcome.Fold(compilerDependencies(context, directory, view), func(struct{}) outcome.Result[compiler.Package] {
		return outcome.Fold(sequence.Traverse(sources, func(source directives.SourceModule) outcome.Result[struct{}] {
			target := filepath.Join(view, "src", filepath.FromSlash(source.Name)+".gleam")
			return outcome.TupleToResult(struct{}{}, createSourceLink(source.Path, target))
		}), func([]struct{}) outcome.Result[compiler.Package] {
			return outcome.Fold(outcome.TupleToResult(struct{}{}, os.MkdirAll(filepath.Join(view, "src"), 0o755)), func(struct{}) outcome.Result[compiler.Package] {
				return outcome.Fold(compiler.Read(context, view), outcome.Ok[compiler.Package], func(cause error) outcome.Result[compiler.Package] {
					if exported, found := errors.AsType[compiler.ExportRefused](cause); found {
						for _, source := range sources {
							from := filepath.Join(view, "src", filepath.FromSlash(source.Name)+".gleam")
							exported.Diagnostics = strings.ReplaceAll(exported.Diagnostics, from, source.Path)
							exported.Output = strings.ReplaceAll(exported.Output, from, source.Path)
						}
						exported.Directory = directory
						return outcome.Err[compiler.Package](exported)
					}
					return outcome.Err[compiler.Package](cause)
				})
			}, outcome.Err[compiler.Package])
		}, outcome.Err[compiler.Package])
	}, outcome.Err[compiler.Package])
}

func createSourceLink(source, target string) error {
	return outcome.FoldOption(outcome.EmptyableToOption(os.MkdirAll(filepath.Dir(target), 0o755)), func(cause error) error { return CompilerSourceLinkFailed{Source: source, Link: target, Cause: cause} }, func() error {
		return outcome.FoldOption(outcome.EmptyableToOption(os.Symlink(source, target)), func(cause error) error { return CompilerSourceLinkFailed{Source: source, Link: target, Cause: cause} }, func() error { return nil })
	})
}
