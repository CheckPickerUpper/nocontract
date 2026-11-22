package effect_test

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/internal/projectfile"
	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

// An empty frontend can generate and check the same Gleam route Monzae serves.
func TestGleamLivenessGeneratesAndChecksAClientInAnEmptyFrontend(t *testing.T) {
	directory := t.TempDir()
	scratchproject.WriteFile(t, directory, "gleam.toml", []byte("name = \"monzae_client\"\nversion = \"0.1.0\"\ntarget = \"erlang\"\n"))
	scratchproject.WriteFile(t, directory, "src/monzae/health/read_liveness.gleam", []byte(`pub type LivenessRequest {
  LivenessRequest
}

pub type LivenessOutcome {
  /// nocontract:status 200
  /// ergleam:not-an-error
  Live
}

/// nocontract:route GET /health/live
pub fn read_liveness(_request: LivenessRequest) -> LivenessOutcome {
  Live
}
`))
	scratchproject.WriteFile(t, directory, "nocontract.json", []byte(`{
  "language": "gleam",
  "packages": ["monzae/health/read_liveness"],
  "api": "MonzaeApi",
  "openapi": {"file": "monzae_api.openapi.json", "version": "0.1.0"},
  "typescript": "client/monzae_api.generated.ts",
  "server": "server"
}`))
	project := filepath.Join(directory, "nocontract.json")
	generated := running.Run(running.Request{Arguments: []string{"generate", "-project", project}, Context: t.Context()})
	if _, written := generated.(running.FilesWritten); !written {
		t.Fatalf("generating the Gleam liveness client: %s", strings.Join(generated.Lines(), "\n"))
	}
	checked := running.Run(running.Request{Arguments: []string{"check", "-project", project}, Context: t.Context()})
	if _, current := checked.(running.FilesCurrent); !current {
		t.Fatalf("checking the Gleam liveness client: %s", strings.Join(checked.Lines(), "\n"))
	}
}

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

func runShopWebCommand(t *testing.T, web, program string, arguments ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), program, arguments...)
	command.Dir = web
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	outcome.ForEachOption(outcome.EmptyableToOption(command.Run()), func(cause error) {
		t.Fatalf("%s %s in %s: %v\nstdout:\n%s\nstderr:\n%s", program, strings.Join(arguments, " "), web, cause, &stdout, &stderr)
	})
}

// The shop checks and runs the same client that NoContract writes.
func TestTheShopsGeneratedClientTypeChecksAndPassesItsTests(t *testing.T) {
	t.Parallel()
	projectPath := scratchproject.PathOrStop(t, "locating the shop's project file",
		outcome.TupleToResult(filepath.Abs(filepath.Join("..", "testdata", "go", "shop", "nocontract.json"))))
	projectfile.Read(projectPath, func(project projectfile.Project) struct{} {
		web := filepath.Join(project.Directory(), "web")
		readWebProject(t, web)
		runShopWebCommand(t, web, "bun", "install", "--frozen-lockfile")
		current := running.Run(running.Request{Arguments: []string{"check", "-project", projectPath}, Context: t.Context()})
		if current.ExitCode() != 0 {
			t.Fatalf("checking the generated shop client: %v", current.Lines())
		}
		runShopWebCommand(t, web, "bun", "run", "typecheck")
		runShopWebCommand(t, web, "bun", "test")
		return struct{}{}
	}, func(cause error) struct{} {
		t.Fatal(strings.Join((running.ProjectUnreadable{Path: projectPath, Cause: cause}).Lines(), "\n"))
		return struct{}{}
	}, func(refusals []assembling.Refusal) struct{} {
		t.Fatal(strings.Join((running.ProjectFileRefused{Path: projectPath, Refusals: refusals}).Lines(), "\n"))
		return struct{}{}
	})
}
