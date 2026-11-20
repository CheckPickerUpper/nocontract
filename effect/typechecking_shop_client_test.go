package effect_test

import (
	"bytes"
	"encoding/json"
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

func runShopWebCommand(t *testing.T, web, program string, arguments ...string) []byte {
	t.Helper()
	command := exec.CommandContext(t.Context(), program, arguments...)
	command.Dir = web
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	outcome.ForEachOption(outcome.EmptyableToOption(command.Run()), func(cause error) {
		t.Fatalf("%s %s in %s: %v\nstdout:\n%s\nstderr:\n%s", program, strings.Join(arguments, " "), web, cause, &stdout, &stderr)
	})
	return stdout.Bytes()
}

func writeShopOpenAPITypecheckProject(t *testing.T, web string, generatedClient []byte) string {
	t.Helper()
	directory := t.TempDir()
	dependencies := filepath.Join(web, "node_modules")
	linkedDependencies := filepath.Join(directory, "node_modules")
	scratchproject.StopOn(t, "linking shop dependencies from "+dependencies+" to "+linkedDependencies,
		os.Symlink(dependencies, linkedDependencies))

	contractName := "shop_openapi_client_contract.mts"
	contractPath := filepath.Join(web, contractName)
	contract := scratchproject.BytesOrStop(t, "reading the OpenAPI shop client contract "+contractPath,
		outcome.TupleToResult(os.ReadFile(contractPath)))
	projectName := "tsconfig.json"
	project := struct {
		Extends string   `json:"extends"`
		Files   []string `json:"files"`
	}{Extends: filepath.Join(web, projectName), Files: []string{contractName}}
	projectJSON := scratchproject.BytesOrStop(t, "encoding the OpenAPI shop type-check project for "+contractPath,
		outcome.TupleToResult(json.Marshal(project)))
	for _, file := range []struct {
		name     string
		contents []byte
	}{
		{name: "shop_openapi_client.generated.mts", contents: generatedClient},
		{name: contractName, contents: contract},
		{name: projectName, contents: projectJSON},
	} {
		path := filepath.Join(directory, file.name)
		scratchproject.StopOn(t, "writing the OpenAPI shop type-check file "+path, os.WriteFile(path, file.contents, 0o600))
	}
	return filepath.Join(directory, projectName)
}

// The shop type-checks its committed and OpenAPI-generated clients and tests that 64-bit values
// survive decoding and encoding.
func TestTheShopsGeneratedClientTypeChecksAndPassesItsTests(t *testing.T) {
	t.Parallel()
	projectPath := scratchproject.PathOrStop(t, "locating the shop's project file",
		outcome.TupleToResult(filepath.Abs(filepath.Join("..", "testdata", "go", "shop", "nocontract.json"))))
	projectfile.Read(projectPath, func(project projectfile.Project) struct{} {
		web := filepath.Join(project.Directory(), "web")
		spec := project.OpenAPI().File()
		readWebProject(t, web)
		scratchproject.BytesOrStop(t, "reading the shop's OpenAPI document "+spec, outcome.TupleToResult(os.ReadFile(spec)))

		runShopWebCommand(t, web, "pnpm", "install", "--frozen-lockfile")
		generatedClient := runShopWebCommand(t, web, "bun", "run", "--silent", "generate:openapi",
			"--spec", spec, "--name", project.API().Name.Text())
		typecheckProject := writeShopOpenAPITypecheckProject(t, web, generatedClient)
		runShopWebCommand(t, web, "bun", "run", "tsc", "--project", typecheckProject)
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
