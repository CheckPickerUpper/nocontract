package gleam_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam"
	"github.com/CheckPickerUpper/nocontract/gleam/bindings"
	"github.com/CheckPickerUpper/nocontract/gleam/directives"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/outcome"
)

func TestCompilerAttachmentAndUnrepresentableShapesAreRefused(t *testing.T) {
	answer := "pub type Answer {\n/// ergleam:not-an-error\n/// nocontract:status 200\nAnswered(value: Int)\n}\n"
	route := "/// nocontract:route POST /values\npub fn read(_input: Input) -> Answer { Answered(value: 1) }\n"
	allModules := []string{"./..."}
	tests := []struct {
		name     string
		source   string
		packages []string
		check    func(*testing.T, error)
	}{
		{name: "a typo on one handler is refused even when another route is valid", source: "pub type Input { Input(value: Int) }\n" + answer + route + "/// nocontract:rouet POST /missing\npub fn missing(_input: Input) -> Answer { Answered(value: 2) }\n", packages: allModules, check: func(t *testing.T, cause error) {
			refused, exact := errors.AsType[directives.UnknownDirective](cause)
			if !exact || refused.Text != "nocontract:rouet POST /missing" || filepath.Base(refused.Position.File) != "reader_proof.gleam" || refused.Position.Line != 10 {
				t.Fatalf("refusal = %v; want the misspelled directive and its function location", cause)
			}
		}},
		{name: "an unknown module is refused even when another selection has routes", source: "pub type Input { Input(value: Int) }\n" + answer + route, packages: []string{"reader_proof", "reader_typo"}, check: func(t *testing.T, cause error) {
			refused, exact := errors.AsType[gleam.ModuleNotFound](cause)
			if !exact || filepath.Base(refused.ProjectFile) != "gleam.toml" || refused.Module != "reader_typo" || !slices.Equal(refused.Available, []string{"reader_proof"}) {
				t.Fatalf("refusal = %v; want the requested module and compiler's module names", cause)
			}
		}},
		{name: "an invalid permanent code identifies its constructor", source: "pub type Input { Input(value: Int) }\npub type Other {\nMissingEarlier\n}\npub fn decoy() -> String { \"\nMissing\n\" }\npub type Answer {\n/// ergleam:not-an-error\n/// nocontract:status 200\nAnswered(value: Int)\n/// ergleam:code SH!-1001\n/// nocontract:status 404\nMissing\n}\n" + route, packages: allModules, check: func(t *testing.T, cause error) {
			constructor, located := errors.AsType[gleam.ConstructorCodeRefused](cause)
			prefix, refused := errors.AsType[native.FailureCodePrefixInvalid](cause)
			if !located || constructor.Constructor != "Missing" || filepath.Base(constructor.Declaration.Position.File) != "reader_proof.gleam" || constructor.Declaration.Position.Line != 14 || constructor.Marker != "ergleam:code SH!-1001" || !refused || prefix.Written != "SH!-1001" || prefix.Problem.Text != "SH!" {
				t.Fatalf("refusal = %v; want the complete code and constructor location", cause)
			}
		}},
		{name: "directive inside a string has no compiler attachment", source: "pub type Input { Input(value: Int) }\n" + answer + "pub fn read(_input: Input) -> Answer { Answered(value: 1) }\npub fn misleading() -> String { \"\n/// nocontract:route POST /forged\npub fn read(_input: Input) -> Answer {\n\" }\n", packages: allModules, check: func(t *testing.T, cause error) {
			refused, exact := errors.AsType[gleam.DirectiveRefused](cause)
			if !exact || refused.Requirement != "at least one selected public route" {
				t.Fatalf("refusal=%+v", cause)
			}
		}},
		{name: "an undecided labelled outcome cannot become a forwarded member", source: "pub type Input { Input(value: Int) }\npub type Answer {\n/// ergleam:not-an-error\n/// nocontract:status 200\nAnswered(value: Int)\nUnmarked(value: Int)\n}\n" + route, packages: allModules, check: func(t *testing.T, cause error) {
			refused, exact := errors.AsType[gleam.DirectiveRefused](cause)
			if !exact || refused.Requirement != "an unlabelled positional parameter on an unmarked answer wrapper" {
				t.Fatalf("refusal=%+v", cause)
			}
		}},
		{name: "a heterogeneous tuple cannot become a homogeneous fixed list", source: "pub type Input { Input(body: #(Int, String)) }\n" + answer + route, packages: allModules, check: func(t *testing.T, cause error) {
			refused, exact := errors.AsType[gleam.Unsupported](cause)
			if !exact || refused.Requirement != "identical resolved tuple element types" {
				t.Fatalf("refusal=%+v", cause)
			}
		}},
		{name: "two enum constructors cannot share a wire spelling", source: "pub type Currency {\n/// nocontract:literal CAD\nFirst\n/// nocontract:literal CAD\nSecond\n}\npub type Input { Input(currency: Currency) }\n" + answer + route, packages: allModules, check: func(t *testing.T, cause error) {
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
			request := native.Request{Directory: directory, Packages: test.packages, API: assembling.NamedAPI{Name: apiName.Name, Position: assembling.Position{File: filepath.Join(directory, "gleam.toml"), Line: 1, Column: 1}}, Server: filepath.Join(directory, "server")}
			outcome.Fold(gleam.Read(t.Context(), request), func(project native.Project) struct{} {
				t.Fatalf("unrepresentable route was accepted: %+v", project.Declared)
				return struct{}{}
			}, func(cause error) struct{} { test.check(t, cause); return struct{}{} })
		})
	}
}

func TestGeneratedServerFolderMustNameAGleamModule(t *testing.T) {
	directory := createCompilerProject(t, "pub type Input { Input }\npub type Answer {\n/// ergleam:not-an-error\n/// nocontract:status 200\nLive\n}\n/// nocontract:route GET /live\npub fn read(_input: Input) -> Answer { Live }\n")
	apiName, named := description.ParseAPIName("ExampleApi").(description.APINameRead)
	if !named {
		t.Fatal("ExampleApi did not parse")
	}
	request := native.Request{Directory: directory, Packages: []string{"reader_proof"}, API: assembling.NamedAPI{Name: apiName.Name, Position: assembling.Position{File: filepath.Join(directory, "gleam.toml"), Line: 1, Column: 1}}, Server: filepath.Join(directory, "src", "My-Server")}
	outcome.Fold(gleam.Read(t.Context(), request), func(project native.Project) struct{} {
		switch checked := assembling.Assemble(project.Declared).(type) {
		case assembling.APIAssembled:
			outcome.Fold(project.Generate(checked), func(files []native.File) struct{} {
				t.Fatalf("invalid module folder produced %d generated files", len(files))
				return struct{}{}
			}, func(cause error) struct{} {
				refused, exact := errors.AsType[bindings.ServerModuleNameRefused](cause)
				if !exact || refused.Source != filepath.Join(directory, "src") || refused.Server != request.Server || refused.Relative != "My-Server" || refused.Component != "My-Server" || refused.Index != 0 {
					t.Fatalf("refusal = %+v; want the configured folder and invalid module component", cause)
				}
				return struct{}{}
			})
		case assembling.APIRefused:
			t.Fatalf("valid route was refused: %+v", checked)
		case nil:
			t.Fatal("assembly returned no outcome")
		}
		return struct{}{}
	}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
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
