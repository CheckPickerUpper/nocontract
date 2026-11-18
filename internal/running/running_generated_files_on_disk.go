package running

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/CheckPickerUpper/outcome"
	"github.com/creachadair/atomicfile"
	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
)

// FolderUnreadable is a folder of the project the run could not list, with the operating system's
// error. A file NoContract wrote could be inside it, so the run stops.
//
//ergo:code NC-1127
//ergo:message The folder {Path} could not be read.
type FolderUnreadable struct {
	Path  string
	Cause error
}

// FileUnreadable is a file the run could not read, with the operating system's error: a Go file of
// the project, whose first line says whether NoContract wrote it, or a file at a path generate
// writes.
//
//ergo:code NC-1128
//ergo:message The file {Path} could not be read.
type FileUnreadable struct {
	Path  string
	Cause error
}

// FilesNotGenerated is a run that found Go files the project wrote at paths generate writes: each
// begins with no generated header, so generate leaves it as it is.
//
//ergo:code NC-1129
//ergo:message These files are where nocontract generate writes its own, and NoContract did not write them; move or rename them:
type FilesNotGenerated struct{ Paths []string }

func (FolderUnreadable) isOutcome()  {}
func (FileUnreadable) isOutcome()    {}
func (FilesNotGenerated) isOutcome() {}

// ExitCode is 2: the run could not read the project.
func (FolderUnreadable) ExitCode() int { return 2 }

// ExitCode is 2: the run could not read the project.
func (FileUnreadable) ExitCode() int { return 2 }

// ExitCode is 1: the project's files must move.
func (FilesNotGenerated) ExitCode() int { return 1 }

// Lines is the failure and its cause.
func (unreadable FolderUnreadable) Lines() []string { return described(unreadable) }

// Lines is the failure and its cause.
func (unreadable FileUnreadable) Lines() []string { return described(unreadable) }

// Lines is the failure, then each file the project wrote.
func (taken FilesNotGenerated) Lines() []string { return append(described(taken), taken.Paths...) }

// goFileWriter is who wrote a Go file, read from how it begins: NoContract begins every file it
// writes with its generated header.
//
//sumtype:decl
type goFileWriter interface{ isGoFileWriter() }

// writtenByNoContract is a Go file beginning with NoContract's generated header.
//
//ergo:not-an-error
type writtenByNoContract struct{}

// writtenByProject is a Go file beginning any other way.
//
//ergo:not-an-error
type writtenByProject struct{}

func (writtenByNoContract) isGoFileWriter() {}
func (writtenByProject) isGoFileWriter()    {}

// readGoFileWriter is who wrote the Go file that begins with start.
func readGoFileWriter(start []byte) goFileWriter {
	if bytes.HasPrefix(start, []byte(description.GeneratedHeader)) {
		return writtenByNoContract{}
	}
	return writtenByProject{}
}

// generatedGoFiles hands next the path of every Go file under directory that NoContract wrote,
// read from how each begins, or stops at the first folder the walk cannot list or Go file it
// cannot read. It leaves out the folders the go command builds no package from: those whose name
// begins with a dot or an underscore, testdata, vendor and node_modules.
func generatedGoFiles(directory string, next func([]string) Outcome) Outcome {
	found, stopped := []string{}, outcome.None[Outcome]()
	visit := func(path string, entry fs.DirEntry) error {
		name := entry.Name()
		switch {
		case entry.IsDir() && path != directory &&
			(strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "vendor" || name == "node_modules"):
			return filepath.SkipDir
		case entry.IsDir() || filepath.Ext(name) != ".go":
			return nil
		}
		return outcome.Fold(fileStart(path, int64(len(description.GeneratedHeader))), func(start []byte) error {
			switch readGoFileWriter(start).(type) {
			case writtenByNoContract:
				found = append(found, path)
			case writtenByProject, nil:
			}
			return nil
		}, func(cause error) error {
			stopped = outcome.Some[Outcome](FileUnreadable{Path: path, Cause: cause})
			return filepath.SkipAll
		})
	}
	walked := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		return outcome.FoldOption(outcome.EmptyableToOption(walkErr), func(cause error) error {
			stopped = outcome.Some[Outcome](FolderUnreadable{Path: path, Cause: cause})
			return filepath.SkipAll
		}, func() error { return visit(path, entry) })
	})
	// The walk returns only what its function returns, which stops it with SkipAll alone.
	return outcome.FoldOption(outcome.EmptyableToOption(walked), func(cause error) Outcome {
		return FolderUnreadable{Path: directory, Cause: cause}
	}, func() Outcome {
		return outcome.FoldOption(stopped, func(failure Outcome) Outcome { return failure }, func() Outcome { return next(found) })
	})
}

// fileStart is the first length bytes of the file at path, or all of it when it is shorter.
func fileStart(path string, length int64) outcome.Result[[]byte] {
	return outcome.Fold(outcome.TupleToResult(os.Open(path)), func(file *os.File) outcome.Result[[]byte] {
		start, readFailure := io.ReadAll(io.LimitReader(file, length))
		return outcome.TupleToResult(start, errors.Join(readFailure, file.Close()))
	}, outcome.Err[[]byte])
}

// fileOnDisk is one file generate owns, and what its path holds now: nothing, or a file generate
// may overwrite, with its content.
type fileOnDisk struct {
	file generatedFile
	held mo.Option[[]byte]
}

// pathHolding is what the disk holds at a path generate writes.
//
//sumtype:decl
type pathHolding interface{ isPathHolding() }

// pathEmpty is a path holding no file.
//
//ergo:not-an-error
type pathEmpty struct{}

// pathOwned is a path holding a file generate may overwrite: a Go file NoContract wrote, or the
// TypeScript or OpenAPI file at the path the project file names for it.
//
//ergo:not-an-error
type pathOwned struct{ content []byte }

// pathTaken is a path holding a Go file the project wrote.
//
//ergo:not-an-error
type pathTaken struct{}

// pathUnreadable is a path holding a file the run cannot read, with the operating system's error.
//
//ergo:not-an-error
type pathUnreadable struct{ cause error }

func (pathEmpty) isPathHolding()      {}
func (pathOwned) isPathHolding()      {}
func (pathTaken) isPathHolding()      {}
func (pathUnreadable) isPathHolding() {}

// readHolding is what the disk holds at path. A Go file there is generate's when NoContract wrote
// it; the project file names the other paths generate writes, which makes the file at each one
// generate's.
func readHolding(path string) pathHolding {
	return outcome.Fold(outcome.TupleToResult(os.ReadFile(path)), func(content []byte) pathHolding {
		if filepath.Ext(path) != ".go" {
			return pathOwned{content: content}
		}
		switch readGoFileWriter(content).(type) {
		case writtenByNoContract:
			return pathOwned{content: content}
		case writtenByProject, nil:
		}
		return pathTaken{}
	}, func(cause error) pathHolding {
		if errors.Is(cause, fs.ErrNotExist) {
			return pathEmpty{}
		}
		return pathUnreadable{cause: cause}
	})
}

// onDisk hands next each file with what its path holds. It stops at the first path holding a file
// it cannot read, and refuses every Go file the project wrote at one of the paths.
func onDisk(files []generatedFile, next func([]fileOnDisk) Outcome) Outcome {
	read, taken := []fileOnDisk{}, []string{}
	for _, file := range files {
		switch held := readHolding(file.path).(type) {
		case pathEmpty:
			read = append(read, fileOnDisk{file: file, held: mo.None[[]byte]()})
		case pathOwned:
			read = append(read, fileOnDisk{file: file, held: mo.Some(held.content)})
		case pathTaken:
			taken = append(taken, file.path)
		case pathUnreadable:
			return FileUnreadable{Path: file.path, Cause: held.cause}
		case nil:
		}
	}
	if len(taken) > 0 {
		return FilesNotGenerated{Paths: taken}
	}
	return next(read)
}

// current is whether the file's path holds what generate writes there: its content, or nothing for
// a file that must not exist.
func (file fileOnDisk) current() bool {
	return mo.Fold(file.file.content, func(content []byte) bool {
		return mo.Fold(file.held, func(held []byte) bool { return bytes.Equal(held, content) }, func(error) bool { return false })
	}, func(error) bool {
		return mo.Fold(file.held, func([]byte) bool { return false }, func(error) bool { return true })
	})
}

// compareFiles is FilesCurrent when every file's path holds what generate writes there, and
// FilesStale listing every one that does not.
func compareFiles(files []fileOnDisk) Outcome {
	stale, current := []string{}, []string{}
	for _, file := range files {
		if file.current() {
			current = append(current, file.file.path)
			continue
		}
		stale = append(stale, file.file.path)
	}
	if len(stale) > 0 {
		return FilesStale{Paths: stale}
	}
	return FilesCurrent{Paths: current}
}

func writeFiles(files []fileOnDisk) Outcome {
	return writeEach(writeProgress{all: files, remaining: files, written: []string{}})
}

// writeProgress is a generate's progress through its files: every file, those not yet written,
// and the paths written or deleted so far.
type writeProgress struct {
	all       []fileOnDisk
	remaining []fileOnDisk
	written   []string
}

// next is the progress past the first remaining file, having written it or not.
func (progress writeProgress) next(written []string) writeProgress {
	return writeProgress{all: progress.all, remaining: progress.remaining[1:], written: written}
}

// writeEach writes or deletes each remaining file that is not current, stopping at the first
// that cannot be.
func writeEach(progress writeProgress) Outcome {
	if len(progress.remaining) == 0 {
		if len(progress.written) == 0 {
			return compareFiles(progress.all)
		}
		return FilesWritten{Paths: progress.written}
	}
	file := progress.remaining[0]
	if file.current() {
		return writeEach(progress.next(progress.written))
	}
	path := file.file.path
	failure := mo.Fold(file.file.content, func(content []byte) error {
		return mo.Fold(mo.EmptyableToOption(os.MkdirAll(filepath.Dir(path), 0o755)), func(cause error) error { return cause },
			func(error) error { return atomicfile.WriteData(path, content, 0o644) })
	}, func(error) error { return os.Remove(path) })
	return mo.Fold(mo.EmptyableToOption(failure), func(cause error) Outcome {
		return FileNotWritten{Path: path, Cause: cause}
	}, func(error) Outcome { return writeEach(progress.next(append(progress.written, path))) })
}
