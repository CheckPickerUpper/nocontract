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

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/openapi"
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

// FilesNotGenerated is a run that found files the project wrote at paths generate writes: each
// has no NoContract ownership marker, so generate leaves it as it is.
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
		case !entry.Type().IsRegular() || filepath.Ext(name) != ".go":
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
	path    string
	content outcome.Option[[]byte]
	held    outcome.Option[fileContents]
}

type claimedOutput struct {
	file generatedFile
	held outcome.Option[fileContents]
}

// pathHolding is what the disk holds at a path generate writes.
//
//sumtype:decl
type pathHolding interface{ isPathHolding() }

// pathEmpty is a path holding no file.
//
//ergo:not-an-error
type pathEmpty struct{}

// pathOwned is a path holding a regular file with the ownership marker for its output format.
//
//ergo:not-an-error
type pathOwned struct{ contents fileContents }

type fileContents struct {
	bytes []byte
	mode  fs.FileMode
}

// pathTaken is a path holding a file the project wrote.
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

// outputOwner is the evidence that a file's current contents belong to NoContract.
//
//sumtype:decl
type outputOwner interface{ isOutputOwner() }

//ergo:not-an-error
type headedOutput struct {
	header string
	source string
}

//ergo:not-an-error
type openAPIOutput struct{}

func (headedOutput) isOutputOwner()  {}
func (openAPIOutput) isOutputOwner() {}

func goFileAt(path string, content []byte) generatedFile {
	return generatedFile{path: path, content: outcome.Some(renderedOutput(content)), owner: headedOutput{header: description.GeneratedHeader, source: "generated source"}}
}

// readHolding checks ownership before replacing a file.
func readHolding(file generatedFile) pathHolding {
	return outcome.Fold(outcome.TupleToResult(os.Lstat(file.path)), func(info fs.FileInfo) pathHolding {
		if !info.Mode().IsRegular() {
			return pathTaken{}
		}
		return outcome.Fold(outcome.TupleToResult(os.ReadFile(file.path)), func(content []byte) pathHolding {
			if ownsContent(file.owner, content) {
				return pathOwned{contents: fileContents{bytes: content, mode: info.Mode()}}
			}
			return pathTaken{}
		}, func(cause error) pathHolding { return pathUnreadable{cause: cause} })
	}, func(cause error) pathHolding {
		if errors.Is(cause, fs.ErrNotExist) {
			return pathEmpty{}
		}
		return pathUnreadable{cause: cause}
	})
}

func ownsContent(owner outputOwner, content []byte) bool {
	switch marker := owner.(type) {
	case headedOutput:
		return bytes.HasPrefix(content, []byte(marker.header))
	case openAPIOutput:
		return openapi.IsGenerated(content)
	case nil:
	}
	return false
}

// onDisk hands next each file with what its path holds. It stops at the first path holding a file
// it cannot read, and refuses every file the project wrote at one of the paths.
func onDisk(files []generatedFile, next func([]fileOnDisk) Outcome) Outcome {
	read, taken := []claimedOutput{}, []string{}
	for _, file := range files {
		switch held := readHolding(file).(type) {
		case pathEmpty:
			read = append(read, claimedOutput{file: file, held: outcome.None[fileContents]()})
		case pathOwned:
			read = append(read, claimedOutput{file: file, held: outcome.Some(held.contents)})
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
	return renderClaimedOutputs(read, nil, next)
}

// current is whether the file's path holds what generate writes there: its content, or nothing for
// a file that must not exist.
func (file fileOnDisk) current() bool {
	return outcome.FoldOption(file.content, func(content []byte) bool {
		return outcome.FoldOption(file.held, func(held fileContents) bool { return bytes.Equal(held.bytes, content) }, func() bool { return false })
	}, func() bool {
		return outcome.FoldOption(file.held, func(fileContents) bool { return false }, func() bool { return true })
	})
}

// compareFiles is FilesCurrent when every file's path holds what generate writes there, and
// FilesStale listing every one that does not.
func compareFiles(files []fileOnDisk) Outcome {
	stale, current := []string{}, []string{}
	for _, file := range files {
		if file.current() {
			current = append(current, file.path)
			continue
		}
		stale = append(stale, file.path)
	}
	if len(stale) > 0 {
		return FilesStale{Paths: stale}
	}
	return FilesCurrent{Paths: current}
}
