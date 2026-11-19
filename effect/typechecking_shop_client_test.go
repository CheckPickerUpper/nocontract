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
	outcome.ForEachOption(outcome.EmptyableToOption(os.Symlink(dependencies, linkedDependencies)), func(cause error) {
		t.Fatalf("linking shop dependencies from %s to %s: %v", dependencies, linkedDependencies, cause)
	})

	contractName := "shop_openapi_client_contract.mts"
	contractPath := filepath.Join(web, contractName)
	contract := outcome.Fold(outcome.TupleToResult(os.ReadFile(contractPath)), func(contents []byte) []byte {
		return contents
	}, func(cause error) []byte {
		t.Fatalf("reading the OpenAPI shop client contract %s: %v", contractPath, cause)
		return []byte{}
	})
	projectName := "tsconfig.json"
	project := struct {
		Extends string   `json:"extends"`
		Files   []string `json:"files"`
	}{Extends: filepath.Join(web, projectName), Files: []string{contractName}}
	var projectJSON bytes.Buffer
	outcome.ForEachOption(outcome.EmptyableToOption(json.NewEncoder(&projectJSON).Encode(project)), func(cause error) {
		t.Fatalf("encoding the OpenAPI shop type-check project for %s: %v", contractPath, cause)
	})
	for _, file := range []struct {
		name     string
		contents []byte
	}{
		{name: "shop_openapi_client.generated.mts", contents: generatedClient},
		{name: contractName, contents: contract},
		{name: projectName, contents: projectJSON.Bytes()},
	} {
		path := filepath.Join(directory, file.name)
		outcome.ForEachOption(outcome.EmptyableToOption(os.WriteFile(path, file.contents, 0o600)), func(cause error) {
			t.Fatalf("writing the OpenAPI shop type-check file %s: %v", path, cause)
		})
	}
	return filepath.Join(directory, projectName)
}

// The shop type-checks its committed and OpenAPI-generated clients and tests that 64-bit values
// survive decoding and encoding.
func TestTheShopsGeneratedClientTypeChecksAndPassesItsTests(t *testing.T) {
	t.Parallel()
	shop := filepath.Join("..", "testdata", "go", "shop")
	web := outcome.Fold(outcome.TupleToResult(filepath.Abs(filepath.Join(shop, "web"))), func(path string) string {
		return path
	}, func(cause error) string {
		t.Fatalf("locating the web project for %s: %v", shop, cause)
		return ""
	})
	readWebProject(t, web)
	spec := filepath.Join(shop, "shop_api.openapi.json")
	_, readError := os.ReadFile(spec)
	outcome.ForEachOption(outcome.EmptyableToOption(readError), func(cause error) {
		t.Fatalf("reading the shop's OpenAPI document %s: %v", spec, cause)
	})
	runShopWebCommand(t, web, "pnpm", "install", "--frozen-lockfile")
	generatedClient := runShopWebCommand(t, web, "bun", "run", "--silent", "generate:openapi")
	typecheckProject := writeShopOpenAPITypecheckProject(t, web, generatedClient)
	runShopWebCommand(t, web, "bun", "run", "tsc", "--project", typecheckProject)
	runShopWebCommand(t, web, "bun", "test")
}
