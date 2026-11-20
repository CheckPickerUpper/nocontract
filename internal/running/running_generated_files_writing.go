package running

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CheckPickerUpper/outcome"
)

// preparedOutput holds both revisions beside the destination, so publishing or undoing a change
// requires only a rename or removal. An absent revision needs no temporary file.
type preparedOutput struct {
	path  string
	next  outcome.Option[string]
	prior outcome.Option[string]
}

func writeFiles(files []fileOnDisk) Outcome {
	return prepareOutputs(files, nil, func(changes []preparedOutput) Outcome {
		if len(changes) == 0 {
			return compareFiles(files)
		}
		return publishOutputs(changes, 0, make([]string, 0, len(changes)))
	})
}

// prepareOutputs writes every temporary file before changing any destination.
func prepareOutputs(files []fileOnDisk, staged []preparedOutput, finish func([]preparedOutput) Outcome) Outcome {
	for len(files) > 0 && files[0].current() {
		files = files[1:]
	}
	if len(files) == 0 {
		return finish(staged)
	}
	file := files[0]
	path := file.file.path
	return outcome.Fold(stageRevision(path, file.held), func(prior outcome.Option[string]) Outcome {
		defer discardRevision(prior)
		next := outcome.FoldOption(file.file.content, func(content []byte) outcome.Option[fileContents] {
			return outcome.Some(fileContents{bytes: content, mode: 0o644})
		}, outcome.None[fileContents])
		return outcome.Fold(stageRevision(path, next), func(pending outcome.Option[string]) Outcome {
			defer discardRevision(pending)
			change := preparedOutput{path: path, next: pending, prior: prior}
			return prepareOutputs(files[1:], append(staged, change), finish)
		}, func(cause error) Outcome { return FileNotWritten{Path: path, Cause: cause} })
	}, func(cause error) Outcome { return FileNotWritten{Path: path, Cause: cause} })
}

func stageRevision(path string, revision outcome.Option[fileContents]) outcome.Result[outcome.Option[string]] {
	return outcome.FoldOption(revision, func(contents fileContents) outcome.Result[outcome.Option[string]] {
		directory := filepath.Dir(path)
		mkdir := outcome.TupleToResult(directory, os.MkdirAll(directory, 0o755))
		return outcome.Fold(mkdir, func(directory string) outcome.Result[outcome.Option[string]] {
			created := outcome.TupleToResult(os.CreateTemp(directory, ".nocontract-*"))
			return outcome.Fold(created, func(temporary *os.File) outcome.Result[outcome.Option[string]] {
				name := temporary.Name()
				_, writeError := temporary.Write(contents.bytes)
				failure := errors.Join(writeError, temporary.Chmod(contents.mode), temporary.Close())
				return outcome.Fold(outcome.TupleToResult(name, failure), func(path string) outcome.Result[outcome.Option[string]] {
					return outcome.Ok(outcome.Some(path))
				}, func(cause error) outcome.Result[outcome.Option[string]] {
					return outcome.Err[outcome.Option[string]](errors.Join(cause, os.Remove(name)))
				})
			}, outcome.Err[outcome.Option[string]])
		}, outcome.Err[outcome.Option[string]])
	}, func() outcome.Result[outcome.Option[string]] { return outcome.Ok(outcome.None[string]()) })
}

func discardRevision(revision outcome.Option[string]) {
	outcome.ForEachOption(revision, func(path string) { _ = os.Remove(path) })
}

func installRevision(path string, revision outcome.Option[string]) error {
	return outcome.FoldOption(revision, func(staged string) error { return os.Rename(staged, path) }, func() error { return os.Remove(path) })
}

func publishOutput(change preparedOutput) error {
	return outcome.FoldOption(change.prior, func(string) error {
		return installRevision(change.path, change.next)
	}, func() error {
		// A path that was absent must still be absent when publishing it.
		return outcome.FoldOption(change.next, func(staged string) error {
			return os.Link(staged, change.path)
		}, func() error { return nil })
	})
}

func publishOutputs(changes []preparedOutput, published int, written []string) Outcome {
	if published == len(changes) {
		return FilesWritten{Paths: written}
	}
	change := changes[published]
	return outcome.FoldOption(outcome.EmptyableToOption(publishOutput(change)), func(cause error) Outcome {
		for index := published - 1; index >= 0; index-- {
			prior := changes[index]
			outcome.ForEachOption(outcome.EmptyableToOption(installRevision(prior.path, prior.prior)), func(rollback error) {
				cause = errors.Join(cause, fmt.Errorf("restoring %s: %w", prior.path, rollback))
			})
		}
		return FileNotWritten{Path: change.path, Cause: cause}
	}, func() Outcome {
		return publishOutputs(changes, published+1, append(written, change.path))
	})
}
