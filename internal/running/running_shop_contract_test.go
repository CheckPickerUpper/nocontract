package running_test

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/internal/projectfile"
	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

type shopDocument struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Paths map[string]map[string]shopDocumentOperation `json:"paths"`
}

type shopDocumentOperation struct {
	OperationID string                          `json:"operationId"`
	Tags        []string                        `json:"tags"`
	Parameters  []shopDocumentParameter         `json:"parameters"`
	RequestBody *shopDocumentRequestBody        `json:"requestBody"`
	Responses   map[string]shopDocumentResponse `json:"responses"`
}

type shopDocumentParameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
}

type shopDocumentRequestBody struct {
	Required bool `json:"required"`
}

type shopDocumentResponse struct {
	Description string `json:"description"`
}

type shopContract struct {
	path     string
	document shopDocument
	compiler *jsonschema.Compiler
}

func canonicalShopJSON(t *testing.T, content string) string {
	t.Helper()
	value := jsontext.Value(content)
	scratchproject.StopOn(t, "reading JSON "+content, value.Canonicalize(jsontext.CanonicalizeRawInts(false)))
	return string(value)
}

func readShopContract(t *testing.T, path string) shopContract {
	t.Helper()
	content := scratchproject.BytesOrStop(t, "reading the shop contract "+path, outcome.TupleToResult(os.ReadFile(path)))
	document := shopDocument{OpenAPI: "", Info: struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	}{Title: "", Version: ""}, Paths: map[string]map[string]shopDocumentOperation{}}
	scratchproject.StopOn(t, "parsing the shop contract "+path, json.Unmarshal(content, &document))
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	outcome.Fold(outcome.TupleToResult(jsonschema.UnmarshalJSON(bytes.NewReader(content))), func(parsed any) struct{} {
		scratchproject.StopOn(t, "adding the shop contract "+path, compiler.AddResource(path, parsed))
		return struct{}{}
	}, func(cause error) struct{} {
		t.Fatalf("parsing the shop contract %s for validation: %v", path, cause)
		return struct{}{}
	})
	return shopContract{path: path, document: document, compiler: compiler}
}

func readShopProjectContract(t *testing.T, projectPath string, read func(shopContract)) {
	t.Helper()
	projectfile.Read(projectPath, func(project projectfile.Project) struct{} {
		read(readShopContract(t, project.OpenAPI().File()))
		return struct{}{}
	}, func(cause error) struct{} {
		t.Fatal(strings.Join((running.ProjectUnreadable{Path: projectPath, Cause: cause}).Lines(), "\n"))
		return struct{}{}
	}, func(refusals []assembling.Refusal) struct{} {
		t.Fatal(strings.Join((running.ProjectFileRefused{Path: projectPath, Refusals: refusals}).Lines(), "\n"))
		return struct{}{}
	})
}

func compileShopSchema(t *testing.T, contract shopContract, compiled func(*jsonschema.Schema), pointer ...string) {
	t.Helper()
	escaped := []string{}
	for _, part := range pointer {
		escaped = append(escaped, url.PathEscape(strings.NewReplacer("~", "~0", "/", "~1").Replace(part)))
	}
	location := contract.path + "#/" + strings.Join(escaped, "/")
	outcome.Fold(outcome.TupleToResult(contract.compiler.Compile(location)), func(schema *jsonschema.Schema) struct{} {
		compiled(schema)
		return struct{}{}
	}, func(cause error) struct{} {
		t.Fatalf("compiling %s: %v", location, cause)
		return struct{}{}
	})
}

func assertShopContract(t *testing.T, expected, actual shopContract) {
	t.Helper()
	if difference := cmp.Diff(expected.document, actual.document); difference != "" {
		t.Errorf("the shop's operations differ (-go +backend):\n%s", difference)
	}
	for _, operation := range []struct {
		path   string
		method string
		name   string
	}{
		{path: "/shoppers/{shopper_id}/balance", method: "get", name: "balance.readBalance"},
		{path: "/shoppers/{shopper_id}/top-ups", method: "post", name: "balance.topUp"},
		{path: "/shoppers/{shopper_id}/orders", method: "post", name: "orders.place"},
	} {
		methods, foundPath := actual.document.Paths[operation.path]
		if !foundPath {
			t.Fatalf("the contract has no %s", operation.path)
		}
		published, foundMethod := methods[operation.method]
		if !foundMethod {
			t.Fatalf("the contract has no %s %s", operation.method, operation.path)
		}
		if published.OperationID != operation.name {
			t.Errorf("%s %s operationId %q; want %q", operation.method, operation.path, published.OperationID, operation.name)
		}
		for index := range published.Parameters {
			assertShopSchemaEqual(t, expected, actual, "paths", operation.path, operation.method, "parameters", fmt.Sprint(index), "schema")
		}
		outcome.ForEachOption(outcome.PointerToOption(published.RequestBody), func(shopDocumentRequestBody) {
			assertShopSchemaEqual(t, expected, actual, "paths", operation.path, operation.method, "requestBody", "content", "application/json", "schema")
		})
		for status := range published.Responses {
			assertShopSchemaEqual(t, expected, actual, "paths", operation.path, operation.method, "responses", status, "content", "application/json", "schema")
		}
	}
}

func assertShopSchemaEqual(t *testing.T, expected, actual shopContract, pointer ...string) {
	t.Helper()
	compileShopSchema(t, expected, func(left *jsonschema.Schema) {
		compileShopSchema(t, actual, func(right *jsonschema.Schema) {
			if difference := cmp.Diff(left, right, shopSchemaComparisons(*left)...); difference != "" {
				t.Errorf("schema %s differs (-go +backend):\n%s", strings.Join(pointer, "/"), difference)
			}
		}, pointer...)
	}, pointer...)
}

func dereferenceShopSchema(schema *jsonschema.Schema) *jsonschema.Schema {
	return outcome.FoldOption(outcome.EmptyableToOption(schema), func(found *jsonschema.Schema) *jsonschema.Schema {
		return outcome.FoldOption(outcome.EmptyableToOption(found.Ref), dereferenceShopSchema, func() *jsonschema.Schema { return found })
	}, func() *jsonschema.Schema { return schema })
}

func shopSchemaComparisons(schema jsonschema.Schema) []cmp.Option {
	return []cmp.Option{
		cmpopts.IgnoreUnexported(schema),
		cmpopts.IgnoreFields(schema, "Location", "ID"),
		cmp.Comparer(func(left, right jsonschema.Enum) bool { return cmp.Equal(left.Values, right.Values) }),
		cmp.Comparer(func(left, right jsonschema.Format) bool { return left.Name == right.Name }),
		cmpopts.SortSlices(func(left, right string) bool { return left < right }),
		cmp.Comparer(func(left, right jsonschema.Regexp) bool {
			return outcome.EqualOption(outcome.FoldOption(outcome.EmptyableToOption(left), func(pattern jsonschema.Regexp) outcome.Option[string] {
				return outcome.Some(pattern.String())
			}, outcome.None[string]), outcome.FoldOption(outcome.EmptyableToOption(right), func(pattern jsonschema.Regexp) outcome.Option[string] {
				return outcome.Some(pattern.String())
			}, outcome.None[string]))
		}),
		cmp.Comparer(func(left, right *big.Rat) bool {
			return outcome.EqualOption(outcome.FoldOption(outcome.EmptyableToOption(left), func(number *big.Rat) outcome.Option[string] {
				return outcome.Some(number.String())
			}, outcome.None[string]), outcome.FoldOption(outcome.EmptyableToOption(right), func(number *big.Rat) outcome.Option[string] {
				return outcome.Some(number.String())
			}, outcome.None[string]))
		}),
		cmp.FilterValues(func(left, right *jsonschema.Schema) bool {
			return dereferenceShopSchema(left) != left || dereferenceShopSchema(right) != right
		}, cmp.Comparer(func(left, right *jsonschema.Schema) bool {
			return cmp.Equal(dereferenceShopSchema(left), dereferenceShopSchema(right), shopSchemaComparisons(schema)...)
		})),
	}
}

func assertShopBodySchema(t *testing.T, contract shopContract, request shopRequestCase) {
	t.Helper()
	outcome.ForEachOption(request.bodySchema, func(expected shopBodySchema) {
		if !jsontext.Value(request.body).IsValid() {
			return
		}
		compileShopSchema(t, contract, func(schema *jsonschema.Schema) {
			outcome.Fold(outcome.TupleToResult(jsonschema.UnmarshalJSON(strings.NewReader(request.body))), func(value any) struct{} {
				outcome.FoldOption(outcome.EmptyableToOption(schema.Validate(value)), func(refusal error) struct{} {
					if expected.accepts {
						t.Errorf("the request schema refuses accepted body %s: %v", request.body, refusal)
					}
					return struct{}{}
				}, func() struct{} {
					if !expected.accepts {
						t.Errorf("the request schema accepts refused body %s", request.body)
					}
					return struct{}{}
				})
				return struct{}{}
			}, func(cause error) struct{} {
				t.Fatalf("parsing request body %s for validation: %v", request.body, cause)
				return struct{}{}
			})
		}, "paths", expected.path, "post", "requestBody", "content", "application/json", "schema")
	})
}

func assertShopResponseSchema(t *testing.T, contract shopContract, request shopRequestCase, answer shopAnswer) {
	t.Helper()
	path, _, _ := strings.Cut(request.path, "?")
	segments := strings.Split(path, "/")
	if len(segments) != 4 {
		return
	}
	documentPath := "/shoppers/{shopper_id}/" + segments[3]
	methods, foundPath := contract.document.Paths[documentPath]
	if !foundPath {
		return
	}
	method := strings.ToLower(request.method)
	operation, foundMethod := methods[method]
	if !foundMethod {
		return
	}
	status := fmt.Sprint(answer.status)
	if _, foundStatus := operation.Responses[status]; !foundStatus {
		t.Fatalf("the contract declares no %s %s response with status %s", method, documentPath, status)
	}
	compileShopSchema(t, contract, func(schema *jsonschema.Schema) {
		outcome.Fold(outcome.TupleToResult(jsonschema.UnmarshalJSON(strings.NewReader(answer.body))), func(value any) struct{} {
			scratchproject.StopOn(t, "validating the response body "+answer.body, schema.Validate(value))
			return struct{}{}
		}, func(cause error) struct{} {
			t.Fatalf("parsing response body %s for validation: %v", answer.body, cause)
			return struct{}{}
		})
	}, "paths", documentPath, method, "responses", status, "content", "application/json", "schema")
}
