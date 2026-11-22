package writing_test

import (
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

func TestAGeneratedListenerBuildsBesideContextAndOutcomeNames(t *testing.T) {
	directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
	scratchproject.WriteFile(t, directory, "server/package_names.go", []byte("package server\n\ntype context struct{}\ntype outcome struct{}\n"))
	generated := running.Run(running.Request{Arguments: []string{"generate", "-project", filepath.Join(directory, "nocontract.json")}, Context: t.Context()})
	if generated.ExitCode() != 0 {
		t.Fatalf("generating the listener beside package names: %s", strings.Join(generated.Lines(), "\n"))
	}
	configuration := &packages.Config{
		Mode:    packages.NeedName | packages.NeedTypes,
		Context: t.Context(), Logf: nil, Dir: directory, Env: os.Environ(), BuildFlags: []string{}, Fset: token.NewFileSet(),
		ParseFile: nil, Tests: false, Overlay: map[string][]byte{},
	}
	outcome.Fold(outcome.TupleToResult(packages.Load(configuration, "./server")), func(loaded []*packages.Package) struct{} {
		for _, checked := range loaded {
			for _, problem := range checked.Errors {
				t.Errorf("building the generated listener: %s", problem)
			}
		}
		return struct{}{}
	}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
}
