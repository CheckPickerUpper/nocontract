package gleam_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/outcome"
)

func TestCompilerAttachmentAndUnrepresentableShapesAreRefused(t *testing.T) {
	answer := "pub type Answer {\n/// ergleam:not-an-error\n/// nocontract:status 200\nAnswered(value: Int)\n}\n"
	route := "/// nocontract:route POST /values\npub fn read(_input: Input) -> Answer { Answered(value: 1) }\n"
	tests := []struct {
		name   string
		source string
		check  func(*testing.T, error)
	}{
		{name: "directive inside a string has no compiler attachment", source: "pub type Input { Input(value: Int) }\n" + answer + "pub fn read(_input: Input) -> Answer { Answered(value: 1) }\npub fn misleading() -> String { \"\n/// nocontract:route POST /forged\npub fn read(_input: Input) -> Answer {\n\" }\n", check: func(t *testing.T, cause error) {
			refused, exact := errors.AsType[gleam.DirectiveRefused](cause)
			if !exact || refused.Requirement != "at least one selected public route" {
				t.Fatalf("refusal=%+v", cause)
			}
		}},
		{name: "an undecided labelled outcome cannot become a forwarded member", source: "pub type Input { Input(value: Int) }\npub type Answer {\n/// ergleam:not-an-error\n/// nocontract:status 200\nAnswered(value: Int)\nUnmarked(value: Int)\n}\n" + route, check: func(t *testing.T, cause error) {
			refused, exact := errors.AsType[gleam.DirectiveRefused](cause)
			if !exact || refused.Requirement != "an unlabelled positional parameter on an unmarked answer wrapper" {
				t.Fatalf("refusal=%+v", cause)
			}
		}},
		{name: "a heterogeneous tuple cannot become a homogeneous fixed list", source: "pub type Input { Input(body: #(Int, String)) }\n" + answer + route, check: func(t *testing.T, cause error) {
			refused, exact := errors.AsType[gleam.Unsupported](cause)
			if !exact || refused.Requirement != "identical resolved tuple element types" {
				t.Fatalf("refusal=%+v", cause)
			}
		}},
		{name: "two enum constructors cannot share a wire spelling", source: "pub type Currency {\n/// nocontract:literal CAD\nFirst\n/// nocontract:literal CAD\nSecond\n}\npub type Input { Input(currency: Currency) }\n" + answer + route, check: func(t *testing.T, cause error) {
			refused, exact := errors.AsType[gleam.LiteralRepeated](cause)
			if !exact || refused.Spelling != "CAD" || refused.First.Name != "First" || refused.Repeated.Name != "Second" || refused.First.Type != refused.Repeated.Type || refused.First.Type.Package != "reader_proof" || refused.First.Type.Module != "reader_proof" || refused.First.Type.Name != "Currency" || refused.First.Position.Line != 3 || refused.Repeated.Position.Line != 5 || refused.First.Position.File != refused.Repeated.Position.File || filepath.Base(refused.First.Position.File) != "reader_proof.gleam" {
				t.Fatalf("refusal=%+v", cause)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := createCompilerProject(t, test.source)
			apiName, named := description.ParseAPIName("ExampleApi").(description.APINameRead)
			if !named {
				t.Fatal("ExampleApi did not parse")
			}
			request := native.Request{Directory: directory, Packages: []string{"./..."}, API: assembling.NamedAPI{Name: apiName.Name, Position: assembling.Position{File: filepath.Join(directory, "gleam.toml"), Line: 1, Column: 1}}, Server: filepath.Join(directory, "server")}
			outcome.Fold(gleam.Read(t.Context(), request), func(project native.Project) struct{} {
				t.Fatalf("unrepresentable route was accepted: %+v", project.Declared)
				return struct{}{}
			}, func(cause error) struct{} { test.check(t, cause); return struct{}{} })
		})
	}
}

func createCompilerProject(t *testing.T, source string) string {
	t.Helper()
	directory := outcome.Fold(outcome.TupleToResult(os.MkdirTemp(os.Getenv("TEST_SCRATCH"), "gleam-reader-*")), func(directory string) string { return directory }, func(cause error) string { t.Fatal(cause); return "" })
	t.Cleanup(func() {
		outcome.ForEachOption(outcome.EmptyableToOption(os.RemoveAll(directory)), func(cause error) { t.Error(cause) })
	})
	outcome.ForEachOption(outcome.EmptyableToOption(os.MkdirAll(filepath.Join(directory, "src"), 0o755)), func(cause error) { t.Fatal(cause) })
	outcome.ForEachOption(outcome.EmptyableToOption(os.WriteFile(filepath.Join(directory, "gleam.toml"), []byte("name = \"reader_proof\"\nversion = \"1.0.0\"\ntarget = \"erlang\"\n"), 0o644)), func(cause error) { t.Fatal(cause) })
	outcome.ForEachOption(outcome.EmptyableToOption(os.WriteFile(filepath.Join(directory, "src", "reader_proof.gleam"), []byte(source), 0o644)), func(cause error) { t.Fatal(cause) })
	return directory
}
