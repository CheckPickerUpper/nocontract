package odin_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/nocontract/odin"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

func TestCompilerFailureRetainsTheCommandAndSourceDiagnostics(t *testing.T) {
	directory := outcome.Fold(outcome.TupleToResult(filepath.Abs("../.scratch")), func(path string) string { return path }, func(cause error) string { t.Fatal(cause); return "" })
	checkError(t, os.MkdirAll(directory, 0o755))
	scratch := outcome.Fold(outcome.TupleToResult(os.MkdirTemp(directory, "odin-compiler-failure-")), func(path string) string { return path }, func(cause error) string { t.Fatal(cause); return "" })
	defer func() { checkError(t, os.RemoveAll(scratch)) }()
	routes := filepath.Join(scratch, "routes")
	checkError(t, os.MkdirAll(routes, 0o755))
	source := filepath.Join(routes, "broken_routes.odin")
	checkError(t, os.WriteFile(source, []byte("package routes\n\n//nocontract:route GET /broken\nRead :: proc(input: MissingInput) -> MissingOutcome {}\n"), 0o644))
	switch name := description.ParseAPIName("BrokenApi").(type) {
	case description.APINameRead:
		outcome.Fold(odin.Read(t.Context(), native.Request{Directory: scratch, Packages: []string{"routes"}, API: assembling.NamedAPI{Name: name.Name, Position: assembling.Position{File: filepath.Join(scratch, "nocontract.json"), Line: 1, Column: 1}}, Server: filepath.Join(scratch, "server")}), func(project native.Project) struct{} {
			t.Fatalf("compiler accepted invalid Odin: %#v", project.Declared)
			return struct{}{}
		}, func(cause error) struct{} {
			outcome.FoldOption(outcome.TupleToOption(errors.AsType[compiler.CommandFailed](cause)), func(failure compiler.CommandFailed) struct{} {
				if failure.Directory != scratch || !slices.Contains(failure.Arguments, routes) || !strings.Contains(failure.Output, source+"(4:") || !strings.Contains(failure.Output, "MissingInput") {
					t.Fatalf("compiler evidence = %#v", failure)
				}
				return struct{}{}
			}, func() struct{} {
				t.Fatalf("failure = %T %v; want compiler command diagnostics", cause, cause)
				return struct{}{}
			})
			return struct{}{}
		})
	case description.APINameNotIdentifier:
		t.Fatal(name)
	case nil:
		t.Fatal("API name returned no outcome")
	}
}

func TestMissingRouteArgumentsAreRefusedBesideAValidRoute(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	routes := filepath.Join(directory, "routes")
	checkError(t, os.MkdirAll(routes, 0o755))
	source := filepath.Join(routes, "example_routes.odin")
	checkError(t, os.WriteFile(source, []byte(`package routes

Input :: struct #all_or_none { value: i64 }

//nocontract:status 200
@(not_an_error)
Answer :: struct #all_or_none { value: i64 }

//nocontract:status 202
@(not_an_error)
Accepted :: struct #all_or_none {}

Outcome :: union #no_nil { Answer, Accepted }

//nocontract:route GET /values
Read :: proc(input: Input) -> Outcome {
	return Answer{value = input.value}
}

//nocontract:route
Missing :: proc(input: Input) -> Outcome {
	return Answer{value = input.value}
}
`), 0o644))
	name, named := description.ParseAPIName("ExampleApi").(description.APINameRead)
	if !named {
		t.Fatal("ExampleApi did not parse")
	}
	request := native.Request{Directory: directory, Packages: []string{"routes"}, API: assembling.NamedAPI{Name: name.Name, Position: assembling.Position{File: filepath.Join(directory, "nocontract.json"), Line: 1, Column: 1}}, Server: filepath.Join(directory, "server")}
	outcome.Fold(odin.Read(t.Context(), request), func(project native.Project) struct{} {
		t.Fatalf("reader silently omitted the invalid route: %+v", project.Declared)
		return struct{}{}
	}, func(cause error) struct{} {
		refused, exact := errors.AsType[odin.RouteArgumentsWrong](cause)
		if !exact || refused.Position.File != source || refused.Position.Line != 21 || refused.Procedure != "Missing" || refused.Written != "//nocontract:route" {
			t.Fatalf("refusal = %v; want Missing's route syntax and location", cause)
		}
		return struct{}{}
	})
}

func checkError(t *testing.T, cause error) {
	t.Helper()
	outcome.ForEachOption(outcome.EmptyableToOption(cause), func(cause error) { t.Fatal(cause) })
}
