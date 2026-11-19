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

// readWebProject exposes the files the package tools read to Go's test cache.
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

// The shop generates a client from OpenAPI with the pinned Effect fork, type-checks the committed
// client, and tests that 64-bit values survive decoding and encoding.
func TestTheShopsGeneratedClientTypeChecksAndPassesItsTests(t *testing.T) {
	t.Parallel()
	shop := filepath.Join("..", "testdata", "go", "shop")
	web := filepath.Join(shop, "web")
	readWebProject(t, web)
	spec := filepath.Join(shop, "shop_api.openapi.json")
	_, readError := os.ReadFile(spec)
	outcome.ForEachOption(outcome.EmptyableToOption(readError), func(cause error) {
		t.Fatalf("reading the shop's OpenAPI document %s: %v", spec, cause)
	})
	commands := []struct {
		program   string
		arguments []string
	}{
		{program: "pnpm", arguments: []string{"install", "--frozen-lockfile"}},
		{program: "bun", arguments: []string{"run", "generate:openapi"}},
		{program: "bun", arguments: []string{"run", "typecheck"}},
		{program: "bun", arguments: []string{"test"}},
	}
	for _, step := range commands {
		command := exec.CommandContext(t.Context(), step.program, step.arguments...)
		command.Dir = web
		output, runError := command.CombinedOutput()
		outcome.ForEachOption(outcome.EmptyableToOption(runError), func(cause error) {
			t.Fatalf("%s %s in %s: %v\n%s", step.program, strings.Join(step.arguments, " "), web, cause, output)
		})
	}
}
