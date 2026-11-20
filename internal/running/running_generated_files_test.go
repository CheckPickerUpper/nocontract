package running_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

// runOn is the command named run on the project in directory.
func runOn(t *testing.T, command, directory string) running.Outcome {
	t.Helper()
	return running.Run(running.Request{Arguments: []string{command, "-project", filepath.Join(directory, "nocontract.json")}, Context: t.Context()})
}

// heldOrStop is what the file at path holds, stopping the test when it cannot be read.
func heldOrStop(t *testing.T, path string) string {
	t.Helper()
	return string(scratchproject.BytesOrStop(t, "reading "+path, outcome.TupleToResult(os.ReadFile(path))))
}

// notesFile is a Go file the project wrote, which another file of its package calls.
const notesFile = "// Package notes keeps the shop's notes.\npackage notes\n\n// Note is the note the shop keeps.\nfunc Note() string { return \"kept\" }\n"

// A Go file the project wrote under the name generate gives its own, in a package generate writes
// nothing into, is built with its package and left as it is.
func TestAGoFileTheProjectWroteUnderAGeneratedNameIsBuiltAndKept(t *testing.T) {
	t.Parallel()
	directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
	kept := filepath.Join(directory, "notes", "nocontract_routes.go")
	scratchproject.WriteFile(t, directory, "notes/nocontract_routes.go", []byte(notesFile))
	scratchproject.WriteFile(t, directory, "notes/notes_reading.go", []byte("package notes\n\n// Read is the note the shop keeps.\nfunc Read() string { return Note() }\n"))
	generated := runOn(t, "generate", directory)
	written, isWritten := generated.(running.FilesWritten)
	if !isWritten || slices.Contains(written.Paths, kept) {
		t.Fatalf("generate answered %v; want the ledger's files written and %s left alone", generated.Lines(), kept)
	}
	if held := heldOrStop(t, kept); held != notesFile {
		t.Fatalf("%s holds\n%s\nwant\n%s", kept, held, notesFile)
	}
}

// A Go file the project wrote at a path generate writes is refused and left as it is.
func TestAGoFileTheProjectWroteWhereGenerateWritesIsRefusedAndKept(t *testing.T) {
	t.Parallel()
	directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
	taken := filepath.Join(directory, "ledger", "nocontract_routes.go")
	const helper = "package ledger\n\n// Kept is a helper the project wrote.\nfunc Kept() int { return 1 }\n"
	scratchproject.WriteFile(t, directory, "ledger/nocontract_routes.go", []byte(helper))
	generated := runOn(t, "generate", directory)
	if refused, isRefused := generated.(running.FilesNotGenerated); !isRefused || !slices.Equal(refused.Paths, []string{taken}) {
		t.Fatalf("generate answered %v; want %s refused as a file the project wrote", generated.Lines(), taken)
	}
	if held := heldOrStop(t, taken); held != helper {
		t.Fatalf("%s holds\n%s\nwant\n%s", taken, held, helper)
	}
}

func TestGenerateRefusesConflictingOutputPathsBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, target := range []struct {
		name  string
		path  string
		other string
		alias bool
	}{
		{name: "equivalent paths", path: "web/../ledger_api.openapi.json", other: "openapi.file", alias: false},
		{name: "project file", path: "nocontract.json", other: "project file", alias: false},
		{name: "linked directory", path: "alias/ledger_api.openapi.json", other: "openapi.file", alias: true},
	} {
		t.Run(target.name, func(t *testing.T) {
			directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
			if target.alias {
				scratchproject.StopOn(t, "linking output folder", os.Symlink(directory, filepath.Join(directory, "alias")))
			}
			projectFile := filepath.Join(directory, "nocontract.json")
			config := strings.Replace(heldOrStop(t, projectFile), "web/ledger_api.generated.ts", target.path, 1)
			scratchproject.WriteFile(t, directory, "nocontract.json", []byte(config))
			result := runOn(t, "generate", directory)
			location := filepath.Join(directory, target.path)
			diagnostic := strings.Join(result.Lines(), "\n")
			if result.ExitCode() == 0 || !strings.Contains(diagnostic, location) || !strings.Contains(diagnostic, "typescript") || !strings.Contains(diagnostic, target.other) {
				t.Errorf("generate = %s; want %s refused for typescript and %s", diagnostic, location, target.other)
			}
			if held := heldOrStop(t, projectFile); held != config {
				t.Error("generate overwrote the project file")
			}
			_, cause := os.Stat(filepath.Join(directory, "ledger", "nocontract_routes.go"))
			if !errors.Is(cause, fs.ErrNotExist) {
				t.Errorf("generated Go binding should still be absent: %v", cause)
			}
		})
	}
}

// A file the run must read and cannot stops it with the operating system's error: a file generate
// writes, which check compares, and a Go file of the project, whose first line says whether
// NoContract wrote it.
func TestAFileTheRunCannotReadStopsItWithTheOperatingSystemsError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		path    string
		content string
	}{
		{name: "the OpenAPI file", path: "ledger_api.openapi.json", content: "{}\n"},
		{name: "a Go file of the project", path: "ledger/ledger_notes.go", content: "package ledger\n"},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()
			directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
			locked := filepath.Join(directory, filepath.FromSlash(each.path))
			scratchproject.WriteFile(t, directory, each.path, []byte(each.content))
			scratchproject.StopOn(t, "locking "+locked, os.Chmod(locked, 0))
			checked := runOn(t, "check", directory)
			unreadable, isUnreadable := checked.(running.FileUnreadable)
			if !isUnreadable || unreadable.Path != locked || !errors.Is(unreadable.Cause, fs.ErrPermission) {
				t.Fatalf("check answered %v; want %s unreadable for want of permission", checked.Lines(), locked)
			}
		})
	}
}

// A folder of the project the run cannot list stops it with the operating system's error, since a
// file NoContract wrote could be inside it.
func TestAFolderTheRunCannotListStopsItWithTheOperatingSystemsError(t *testing.T) {
	t.Parallel()
	directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
	locked := filepath.Join(directory, "locked")
	scratchproject.StopOn(t, "making "+locked, os.Mkdir(locked, 0))
	t.Cleanup(func() { scratchproject.StopOn(t, "unlocking "+locked, os.Chmod(locked, 0o755)) })
	checked := runOn(t, "check", directory)
	unreadable, isUnreadable := checked.(running.FolderUnreadable)
	if !isUnreadable || unreadable.Path != locked || !errors.Is(unreadable.Cause, fs.ErrPermission) {
		t.Fatalf("check answered %v; want %s unreadable for want of permission", checked.Lines(), locked)
	}
}
