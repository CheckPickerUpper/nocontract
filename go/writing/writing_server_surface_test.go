package writing_test

import (
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/tools/go/packages"
)

func TestAnApplicationCannotMountTheRuntimeDispatcher(t *testing.T) {
	directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
	source := "package bypass\n\nimport (\n \"net/http\"\n nocontract \"github.com/CheckPickerUpper/nocontract/go\"\n)\n\nfunc Mount(mux *http.ServeMux) {\n mux.HandleFunc(\"/\", func(writer http.ResponseWriter, request *http.Request) {\n  nocontract.Respond(writer, request, []nocontract.Endpoint{}, []nocontract.Layer{})\n })\n}\n"
	scratchproject.WriteFile(t, directory, "bypass/bypass_mounting.go", []byte(source))
	configuration := &packages.Config{
		Mode:    packages.NeedName | packages.NeedTypes,
		Context: t.Context(), Logf: nil, Dir: directory, Env: os.Environ(), BuildFlags: []string{}, Fset: token.NewFileSet(),
		ParseFile: nil, Tests: false, Overlay: map[string][]byte{},
	}
	outcome.Fold(outcome.TupleToResult(packages.Load(configuration, "./bypass")), func(loaded []*packages.Package) struct{} {
		problems := []string{}
		for _, checked := range loaded {
			for _, problem := range checked.TypeErrors {
				problems = append(problems, problem.Msg)
			}
		}
		if len(problems) != 1 || problems[0] != "undefined: nocontract.Respond" {
			t.Fatalf("outside application type check = %v; want nocontract.Respond unavailable", problems)
		}
		return struct{}{}
	}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
}
