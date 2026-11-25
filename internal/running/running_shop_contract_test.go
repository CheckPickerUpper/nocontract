package running_test

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

type shopDocument struct {
	Paths map[string]map[string]shopDocumentOperation `json:"paths"`
}

type shopDocumentOperation struct {
	OperationID string              `json:"operationId"`
	Responses   map[string]struct{} `json:"responses"`
}

type shopContract struct {
	path     string
	document shopDocument
	compiler *jsonschema.Compiler
}

type shopOperationMatch struct {
	path      string
	method    string
	operation shopDocumentOperation
}

func canonicalShopJSON(t *testing.T, content string) string {
	t.Helper()
	value := jsontext.Value(content)
	scratchproject.StopOn(t, "reading JSON "+content, value.Canonicalize(jsontext.CanonicalizeRawInts(false)))
	return string(value)
}

func readShopContract(t *testing.T, file shopSourceFile) shopContract {
	t.Helper()
	document := shopDocument{Paths: map[string]map[string]shopDocumentOperation{}}
	scratchproject.StopOn(t, "parsing the shop contract "+file.path, json.Unmarshal(file.content, &document))
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	outcome.Fold(outcome.TupleToResult(jsonschema.UnmarshalJSON(bytes.NewReader(file.content))), func(parsed any) struct{} {
		scratchproject.StopOn(t, "adding the shop contract "+file.path, compiler.AddResource(file.path, parsed))
		return struct{}{}
	}, func(cause error) struct{} {
		t.Fatalf("parsing the shop contract %s for validation: %v", file.path, cause)
		return struct{}{}
	})
	return shopContract{path: file.path, document: document, compiler: compiler}
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

func assertShopOperationIDs(t *testing.T, contract shopContract) {
	t.Helper()
	for _, operation := range []struct {
		path   string
		method string
		name   string
	}{
		{path: "/shoppers/{shopper_id}/balance", method: "get", name: "balance.readBalance"},
		{path: "/shoppers/{shopper_id}/top-ups", method: "post", name: "balance.topUp"},
		{path: "/shoppers/{shopper_id}/orders", method: "post", name: "orders.place"},
		{path: "/headers/order-count", method: "get", name: "orders.readOrderCount"},
		{path: "/crashes/{cause}", method: "get", name: "orders.crash"},
	} {
		methods, foundPath := contract.document.Paths[operation.path]
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

func matchShopOperation(contract shopContract, request shopRequestCase) outcome.Option[shopOperationMatch] {
	path, _, _ := strings.Cut(request.path, "?")
	segments := strings.Split(path, "/")
	if len(segments) != 4 {
		return outcome.None[shopOperationMatch]()
	}
	documentPath := "/shoppers/{shopper_id}/" + segments[3]
	methods, foundPath := contract.document.Paths[documentPath]
	if !foundPath {
		return outcome.None[shopOperationMatch]()
	}
	method := strings.ToLower(request.method)
	operation, foundMethod := methods[method]
	if !foundMethod {
		return outcome.None[shopOperationMatch]()
	}
	return outcome.Some(shopOperationMatch{path: documentPath, method: method, operation: operation})
}

func assertShopResponseSchema(t *testing.T, contract shopContract, request shopRequestCase, answer shopAnswer) {
	t.Helper()
	outcome.ForEachOption(matchShopOperation(contract, request), func(matched shopOperationMatch) {
		status := fmt.Sprint(answer.status)
		if _, foundStatus := matched.operation.Responses[status]; !foundStatus {
			t.Fatalf("the contract declares no %s %s response with status %s", matched.method, matched.path, status)
		}
		assertShopResponseValueSchema(t, contract, answer, "paths", matched.path, matched.method, "responses", status, "content", "application/json", "schema")
	})
}

func assertShopResponseValueSchema(t *testing.T, contract shopContract, answer shopAnswer, pointer ...string) {
	t.Helper()
	compileShopSchema(t, contract, func(schema *jsonschema.Schema) {
		outcome.Fold(outcome.TupleToResult(jsonschema.UnmarshalJSON(strings.NewReader(answer.body))), func(value any) struct{} {
			scratchproject.StopOn(t, "validating the response body "+answer.body, schema.Validate(value))
			return struct{}{}
		}, func(cause error) struct{} {
			t.Fatalf("parsing response body %s for validation: %v", answer.body, cause)
			return struct{}{}
		})
	}, pointer...)
}
