package gleam_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/effect"
	"github.com/CheckPickerUpper/nocontract/gleam"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/nocontract/openapi"
	"github.com/CheckPickerUpper/outcome"
	"github.com/google/go-cmp/cmp"
)

func TestDeclaredHeadersPublishTheirCanonicalNameAndScalarType(t *testing.T) {
	outcome.Fold(readHeaderProject(t, `pub type Input {
  Input(order_count: Int)
}

pub type Answer {
  /// ergleam:not-an-error
  /// nocontract:status 200
  HeaderRead(order_count: Int)
}

/// nocontract:route GET /headers/order-count
/// nocontract:header order_count X-Order-Count
pub fn read_headers(input: Input) -> Answer {
  HeaderRead(order_count: input.order_count)
}
`), func(project native.Project) struct{} {
		switch assembled := assembling.Assemble(project.Declared).(type) {
		case assembling.APIAssembled:
			checkHeaderProjection(t, assembled.API)
		case assembling.APIRefused:
			t.Fatalf("declared header was refused: %+v", assembled.Refusals)
		case nil:
			t.Fatal("assembly returned no outcome")
		}
		return struct{}{}
	}, func(cause error) struct{} {
		t.Fatalf("reading the header route: %v", cause)
		return struct{}{}
	})
}

func readHeaderProject(t *testing.T, source string) outcome.Result[native.Project] {
	t.Helper()
	directory := createCompilerProject(t, source)
	name, named := description.ParseAPIName("HeaderApi").(description.APINameRead)
	if !named {
		t.Fatal("HeaderApi did not parse")
	}
	request := native.Request{
		Directory: directory, Packages: []string{"reader_proof"},
		API:    assembling.NamedAPI{Name: name.Name, Position: assembling.Position{File: filepath.Join(directory, "gleam.toml"), Line: 1, Column: 1}},
		Server: filepath.Join(directory, "src", "nocontract"),
	}
	return gleam.Read(t.Context(), request)
}

type headerProjectionDocument struct {
	Paths map[string]map[string]headerProjectionOperation `json:"paths"`
}

type headerProjectionOperation struct {
	Parameters []struct {
		Name     string `json:"name"`
		In       string `json:"in"`
		Required bool   `json:"required"`
		Schema   struct {
			Type   string `json:"type"`
			Format string `json:"format"`
		} `json:"schema"`
	} `json:"parameters"`
}

func checkHeaderProjection(t *testing.T, api description.API) {
	t.Helper()
	document := outcome.Fold(openapi.Render(api, "1.0.0"), func(encoded []byte) []byte { return encoded }, func(cause error) []byte {
		t.Fatal(cause)
		return []byte{}
	})
	projection := headerProjectionDocument{Paths: map[string]map[string]headerProjectionOperation{}}
	outcome.ForEachOption(outcome.EmptyableToOption(json.Unmarshal(document, &projection)), func(cause error) { t.Fatal(cause) })
	parameters := projection.Paths["/headers/order-count"]["get"].Parameters
	if len(parameters) != 1 {
		t.Fatalf("header route parameters = %+v; want one header", parameters)
	}
	header := parameters[0]
	if header.Name != "x-order-count" || header.In != "header" || !header.Required || header.Schema.Type != "string" || header.Schema.Format != "int64" {
		t.Fatalf("header parameter = %+v; want the required canonical header with the declared integer wire type", header)
	}
	client := string(effect.Render(api))
	if !strings.Contains(client, "headers: {\n      [\"x-order-count\"]:") || !strings.Contains(client, "Schema.BigIntFromString") || strings.Contains(client, "query:") {
		t.Fatalf("client did not publish the typed header input:\n%s", client)
	}
}

func TestHeaderDirectivesRefuseMissingAndConflictingNativeFields(t *testing.T) {
	answer := "pub type Answer {\n/// ergleam:not-an-error\n/// nocontract:status 200\nHeaderRead\n}\n"
	for _, test := range []struct {
		name    string
		input   string
		path    string
		field   string
		problem assembling.Problem
	}{
		{name: "a field the input does not declare", input: "order_count", path: "/headers", field: "missing", problem: assembling.HeaderFieldUndeclared{Name: "X-Order-Count", Field: "missing", Available: "order_count"}},
		{name: "a field already read from the path", input: "order_count", path: "/headers/{order_count}", field: "order_count", problem: assembling.HeaderBindingConflict{Name: "X-Order-Count", Field: "order_count", Part: "path", Binding: "order_count"}},
		{name: "the field read from the JSON body", input: "body", path: "/headers", field: "body", problem: assembling.HeaderBindingConflict{Name: "X-Order-Count", Field: "body", Part: "body", Binding: "body"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "pub type Input { Input(" + test.input + ": Int) }\n" + answer +
				"/// nocontract:route POST " + test.path + "\n/// nocontract:header " + test.field + " X-Order-Count\npub fn read_headers(_input: Input) -> Answer { HeaderRead }\n"
			outcome.Fold(readHeaderProject(t, source), func(project native.Project) struct{} {
				t.Fatalf("invalid header binding was accepted: %+v", project.Declared)
				return struct{}{}
			}, func(cause error) struct{} {
				refused, exact := errors.AsType[gleam.HeaderDeclarationRefused](cause)
				if !exact {
					t.Fatalf("refusal = %v; want a typed header declaration refusal", cause)
				}
				if refused.Header.Name != "X-Order-Count" || refused.Header.Field != test.field || refused.Header.Position.Line != 9 || filepath.Base(refused.Header.Position.File) != "reader_proof.gleam" {
					t.Errorf("header declaration = %+v; want the exact written binding and function location", refused.Header)
				}
				if difference := cmp.Diff(test.problem, refused.Problem); difference != "" {
					t.Errorf("header refusal differs (-want +got):\n%s", difference)
				}
				return struct{}{}
			})
		})
	}
}
