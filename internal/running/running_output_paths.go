package running

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"
)

type outputClaim struct {
	file  string
	owner string
}

type generatedPathsOverlap struct {
	location string
	first    outputClaim
	second   outputClaim
}

func (failure generatedPathsOverlap) Error() string {
	return fmt.Sprintf("%s at %q and %s at %q both use %q", failure.first.owner, failure.first.file, failure.second.owner, failure.second.file, failure.location)
}

// plannedGeneratedFiles reserves output locations before generate can replace any of them.
func plannedGeneratedFiles(project string, writes []generatedFile, existing []string) outcome.Result[[]generatedFile] {
	projectClaim := outputClaim{file: project, owner: "project file"}
	return outcome.Fold(outputLocation(project), func(projectEntry string) outcome.Result[[]generatedFile] {
		return outcome.Fold(outcome.TupleToResult(filepath.EvalSymlinks(project)), func(projectSource string) outcome.Result[[]generatedFile] {
			occupied := map[string]outputClaim{projectEntry: projectClaim, projectSource: projectClaim}
			files := slices.Clone(writes)
			for _, path := range existing {
				files = append(files, generatedFile{path: path, content: mo.None[[]byte](), owner: headedOutput{header: description.GeneratedHeader}})
			}
			planned := outcome.Ok(make([]generatedFile, 0, len(files)))
			for _, file := range files {
				planned = outcome.Fold(planned, func(accepted []generatedFile) outcome.Result[[]generatedFile] {
					return outcome.Fold(outputLocation(file.path), func(location string) outcome.Result[[]generatedFile] {
						claim := outputClaim{file: file.path, owner: outputSource(file.owner)}
						if first, taken := occupied[location]; taken {
							// A current artifact takes the place of any planned removal at its path.
							return mo.Fold(file.content, func([]byte) outcome.Result[[]generatedFile] {
								return outcome.Err[[]generatedFile](generatedPathsOverlap{location: location, first: first, second: claim})
							}, func(error) outcome.Result[[]generatedFile] { return outcome.Ok(accepted) })
						}
						occupied[location] = claim
						return outcome.Ok(append(accepted, file))
					}, outcome.Err[[]generatedFile])
				}, outcome.Err[[]generatedFile])
			}
			return outcome.Fold(planned, func(accepted []generatedFile) outcome.Result[[]generatedFile] {
				slices.SortFunc(accepted, func(first, second generatedFile) int { return cmp.Compare(first.path, second.path) })
				return outcome.Ok(accepted)
			}, outcome.Err[[]generatedFile])
		}, outcome.Err[[]generatedFile])
	}, outcome.Err[[]generatedFile])
}

func outputSource(owner outputOwner) string {
	switch source := owner.(type) {
	case declaredOutput:
		return source.setting
	case headedOutput, nil:
	}
	return "generated source"
}

// outputLocation names the entry the atomic write replaces. Directory links matter
// because a rename replaces the final entry itself, including a file link.
func outputLocation(path string) outcome.Result[string] {
	return outcome.Fold(outputDirectory(filepath.Dir(path)), func(directory string) outcome.Result[string] {
		return outcome.Ok(filepath.Join(directory, filepath.Base(path)))
	}, outcome.Err[string])
}

func outputDirectory(path string) outcome.Result[string] {
	return outcome.Fold(outcome.TupleToResult(filepath.EvalSymlinks(path)), outcome.Ok[string], func(cause error) outcome.Result[string] {
		parent := filepath.Dir(path)
		if !errors.Is(cause, fs.ErrNotExist) || parent == path {
			return outcome.Err[string](cause)
		}
		// A new directory is relative to its nearest existing parent. A dangling
		// link is already an entry and cannot be created as a new directory.
		_, entryError := os.Lstat(path)
		if !errors.Is(entryError, fs.ErrNotExist) {
			return outcome.Err[string](cause)
		}
		return outcome.Fold(outputDirectory(parent), func(directory string) outcome.Result[string] {
			return outcome.Ok(filepath.Join(directory, filepath.Base(path)))
		}, outcome.Err[string])
	})
}
