package effect_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"
)

// readWebProject reads every file of the web project outside node_modules. bun reads them in a
// child process, which Go's test cache does not see, so the test reads them itself and a change to
// any of them runs it again.
func readWebProject(t *testing.T, web string) {
	t.Helper()
	walked := filepath.WalkDir(web, func(path string, entry fs.DirEntry, walkError error) error {
		return outcome.FoldOption(outcome.EmptyableToOption(walkError), func(cause error) error { return cause }, func() error {
			if entry.IsDir() && entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if entry.IsDir() {
				return nil
			}
			return outcome.Fold(outcome.TupleToResult(os.ReadFile(path)), func([]byte) error { return nil }, func(cause error) error { return cause })
		})
	})
	outcome.ForEachOption(outcome.EmptyableToOption(walked), func(cause error) { t.Fatalf("reading the web project %s: %v", web, cause) })
}

// The shop example's committed client, and the TypeScript calling it, type-check against the
// effect release the web project pins: a failure a route never answers is a type error in
// catchTag, and a decimal is a type error where the route reads a whole number.
func TestTheShopsGeneratedClientTypeChecks(t *testing.T) {
	t.Parallel()
	bun := outcome.Fold(outcome.TupleToResult(exec.LookPath("bun")), func(path string) string { return path }, func(cause error) string {
		t.Fatalf("looked for bun on PATH to type-check the shop's web project: %v", cause)
		return ""
	})
	web := filepath.Join("..", "testdata", "go", "shop", "web")
	readWebProject(t, web)
	for _, arguments := range [][]string{{"install", "--frozen-lockfile"}, {"run", "typecheck"}} {
		command := exec.CommandContext(t.Context(), bun, arguments...)
		command.Dir = web
		output, runError := command.CombinedOutput()
		outcome.ForEachOption(outcome.EmptyableToOption(runError), func(cause error) {
			t.Fatalf("bun %s in %s: %v\n%s", strings.Join(arguments, " "), web, cause, output)
		})
	}
}
