package gleam_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/outcome"
)

func TestGleamTestsRejectAChangedStatusUntilGeneration(t *testing.T) {
	checkout := scratchproject.PathOrStop(t, "locating NoContract", outcome.TupleToResult(filepath.Abs("..")))
	shop := filepath.Join(checkout, "testdata", "gleam", "shop")
	directory := t.TempDir()
	for _, folder := range []string{"src", "test"} {
		scratchproject.StopOn(t, "copying Gleam shop "+folder, os.CopyFS(filepath.Join(directory, folder), os.DirFS(filepath.Join(shop, folder))))
	}
	generatedTest := filepath.Join(directory, "test", "nocontract_files_test.gleam")
	removed := os.Remove(generatedTest)
	if !errors.Is(removed, os.ErrNotExist) {
		scratchproject.StopOn(t, "clearing the generated test before generation", removed)
	}
	for _, name := range []string{"go.mod", "go.sum", "gleam.toml", "manifest.toml", "nocontract.json"} {
		content := scratchproject.BytesOrStop(t, "reading Gleam shop "+name, outcome.TupleToResult(os.ReadFile(filepath.Join(shop, name))))
		scratchproject.WriteFile(t, directory, name, content)
	}
	module := scratchproject.BytesOrStop(t, "reading the scratch Go module", outcome.TupleToResult(os.ReadFile(filepath.Join(directory, "go.mod"))))
	scratchproject.WriteFile(t, directory, "go.mod", scratchproject.BytesOrStop(t, "pointing the Go tool at NoContract", scratchproject.LocalModule(module, checkout)))
	for _, name := range []string{"gleam.toml", "manifest.toml"} {
		content := scratchproject.BytesOrStop(t, "reading runtime dependency in "+name, outcome.TupleToResult(os.ReadFile(filepath.Join(directory, name))))
		rooted := strings.ReplaceAll(string(content), `"../../../gleam/runtime"`, strconv.Quote(filepath.Join(checkout, "gleam", "runtime")))
		scratchproject.WriteFile(t, directory, name, []byte(rooted))
	}
	project := filepath.Join(directory, "contract ; literal's.json")
	scratchproject.StopOn(t, "naming the checked project", os.Rename(filepath.Join(directory, "nocontract.json"), project))
	generate := func() {
		generated := running.Run(running.Request{Arguments: []string{"generate", "-project", project}, Context: t.Context()})
		if generated.ExitCode() != 0 {
			t.Fatalf("generation exited %d:\n%s", generated.ExitCode(), strings.Join(generated.Lines(), "\n"))
		}
	}
	generate()
	generatedTestContent := scratchproject.BytesOrStop(t, "reading the emitted Gleam test", outcome.TupleToResult(os.ReadFile(generatedTest)))
	statusFile := filepath.Join(directory, "src", "shop", "read_outcome.gleam")
	original := scratchproject.BytesOrStop(t, "reading the declared success status", outcome.TupleToResult(os.ReadFile(statusFile)))
	changed := strings.Replace(string(original), "nocontract:status 200", "nocontract:status 201", 1)
	if changed == string(original) {
		t.Fatal("the fixture has no declared 200 status to change")
	}
	stale := running.FilesStale{Paths: []string{
		filepath.Join(directory, "src", "nocontract", "server.gleam"),
		filepath.Join(directory, "shop_api.openapi.json"),
		filepath.Join(directory, "web", "src", "shop_api.generated.ts"),
	}}
	stages := []struct {
		name       string
		prepare    func()
		exitStatus int
		report     []string
	}{
		{name: "changed status without the emitted test", prepare: func() {
			scratchproject.WriteFile(t, directory, "src/shop/read_outcome.gleam", []byte(changed))
			scratchproject.StopOn(t, "removing the emitted test for the control run", os.Remove(generatedTest))
		}, exitStatus: 0, report: []string{}},
		{name: "changed status", prepare: func() {
			scratchproject.WriteFile(t, directory, "test/nocontract_files_test.gleam", generatedTestContent)
		}, exitStatus: 1, report: stale.Lines()},
		{name: "regenerated files", prepare: generate, exitStatus: 0, report: []string{}},
	}
	for _, stage := range stages {
		stage.prepare()
		command := exec.CommandContext(t.Context(), "gleam", "test")
		command.Dir = directory
		output := &bytes.Buffer{}
		command.Stdout, command.Stderr = output, output
		status := outcome.Fold(outcome.TupleToResult(struct{}{}, command.Run()), func(struct{}) int { return 0 }, func(cause error) int {
			return outcome.FoldOption(outcome.TupleToOption(errors.AsType[*exec.ExitError](cause)), func(exited *exec.ExitError) int { return exited.ExitCode() }, func() int {
				t.Fatalf("%s: %v in %s could not start: %v\n%s", stage.name, command.Args, directory, cause, output)
				return -1
			})
		})
		if status != stage.exitStatus {
			t.Fatalf("%s: %v in %s exited %d; want %d:\n%s", stage.name, command.Args, directory, status, stage.exitStatus, output)
		}
		reported := []string{}
		for _, line := range strings.Split(output.String(), "\n") {
			if strings.HasPrefix(line, "NC-") || strings.HasPrefix(line, directory+string(filepath.Separator)) {
				reported = append(reported, line)
			}
		}
		slices.Sort(reported)
		slices.Sort(stage.report)
		if !slices.Equal(reported, stage.report) {
			t.Fatalf("%s: reported %q; want %q:\n%s", stage.name, reported, stage.report, output)
		}
		t.Logf("%s:\n%s", stage.name, output)
	}
}
