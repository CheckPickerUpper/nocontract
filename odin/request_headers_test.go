package odin_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/nocontract/odin"
	"github.com/CheckPickerUpper/outcome"
	"github.com/google/go-cmp/cmp"
)

type headerRoute struct {
	field  string
	path   string
	header string
}

func TestDeclaredHeadersPublishTheirNativeFieldAndScalarType(t *testing.T) {
	outcome.Fold(readHeaderProject(t, headerRoute{field: "order_count", path: "/headers/order-count", header: "order_count X-Order-Count"}), func(project native.Project) struct{} {
		switch assembled := assembling.Assemble(project.Declared).(type) {
		case assembling.APIAssembled:
			endpoint := assembled.API.Groups[0].Endpoints[0]
			if len(endpoint.Headers) != 1 || len(endpoint.Query) != 0 {
				t.Fatalf("header route = %+v; want one header and no query parameters", endpoint)
			}
			header := endpoint.Headers[0]
			if header.Name.Text() != "x-order-count" || header.Field.Text() != "order_count" {
				t.Fatalf("header binding = %+v; want the canonical header name and native field", header)
			}
			integer, whole := header.Schema.(description.Integer)
			if !whole {
				t.Fatalf("header schema = %T; want an integer", header.Schema)
			}
			if _, signed := integer.Width.(description.Signed64); !signed {
				t.Fatalf("header width = %T; want signed 64-bit", integer.Width)
			}
		case assembling.APIRefused:
			t.Fatalf("header declaration was refused: %+v", assembled.Refusals)
		case nil:
			t.Fatal("assembly returned no outcome")
		}
		return struct{}{}
	}, func(cause error) struct{} {
		t.Fatalf("reading the header route: %v", cause)
		return struct{}{}
	})
}

func TestHeaderDirectivesRefuseMissingAndConflictingNativeFields(t *testing.T) {
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
			outcome.Fold(readHeaderProject(t, headerRoute{field: test.input, path: test.path, header: test.field + " X-Order-Count"}), func(project native.Project) struct{} {
				t.Fatalf("invalid header binding was accepted: %+v", project.Declared)
				return struct{}{}
			}, func(cause error) struct{} {
				refused, exact := errors.AsType[odin.HeaderDeclarationRefused](cause)
				if !exact {
					t.Fatalf("refusal = %v; want a typed header declaration refusal", cause)
				}
				if refused.Name != "X-Order-Count" || refused.Field != test.field || refused.Position.Line != 13 || filepath.Base(refused.Position.File) != "header_routes.odin" {
					t.Errorf("header declaration = %+v; want the exact written binding and procedure location", refused)
				}
				if difference := cmp.Diff(test.problem, refused.Problem); difference != "" {
					t.Errorf("header refusal differs (-want +got):\n%s", difference)
				}
				return struct{}{}
			})
		})
	}
}

func readHeaderProject(t *testing.T, route headerRoute) outcome.Result[native.Project] {
	t.Helper()
	directory := t.TempDir()
	source := "package routes\n\nInput :: struct #all_or_none { " + route.field + ": i64 }\n\n" +
		"//nocontract:status 200\n@(not_an_error)\nHeaderRead :: struct #all_or_none { order_count: i64 }\n\n" +
		"Answer :: union { HeaderRead }\n\n//nocontract:route GET " + route.path + "\n//nocontract:header " + route.header + "\n" +
		"ReadHeaders :: proc(input: Input) -> Answer { return HeaderRead{order_count = input." + route.field + "} }\n"
	scratchproject.WriteFile(t, directory, "routes/header_routes.odin", []byte(source))
	name, named := description.ParseAPIName("HeaderApi").(description.APINameRead)
	if !named {
		t.Fatal("HeaderApi did not parse")
	}
	request := native.Request{
		Directory: directory, Packages: []string{"routes"},
		API:    assembling.NamedAPI{Name: name.Name, Position: assembling.Position{File: filepath.Join(directory, "nocontract.json"), Line: 1, Column: 1}},
		Server: filepath.Join(directory, "server"),
	}
	return odin.Read(t.Context(), request)
}
