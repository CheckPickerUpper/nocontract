package nocontract

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/samber/mo"
)

// TestSharedFailuresAnswerWhatTheirDirectivesDeclare holds nocontract's own failures to the status
// and code their declarations carry, which are what the generator writes into the TypeScript.
func TestSharedFailuresAnswerWhatTheirDirectivesDeclare(t *testing.T) {
	t.Parallel()
	declarations := mo.Fold(mo.TupleToResult(parser.ParseFile(token.NewFileSet(), "shared_failures.go", nil, parser.ParseComments)),
		func(parsed *ast.File) []ast.Decl { return parsed.Decls }, func(cause error) []ast.Decl {
			t.Fatal(cause)
			return []ast.Decl{}
		})
	declared := map[string][2]string{}
	for _, declaration := range declarations {
		group, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || group.Tok != token.TYPE {
			continue
		}
		for _, spec := range group.Specs {
			typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
			if !isTypeSpec {
				continue
			}
			name := typeSpec.Name.Name
			for _, line := range group.Doc.List {
				status, isStatus := strings.CutPrefix(line.Text, "//nocontract:status ")
				code, isCode := strings.CutPrefix(line.Text, "//ergo:code ")
				entry := declared[name]
				if isStatus {
					entry[0] = status
				}
				if isCode {
					entry[1] = code
				}
				declared[name] = entry
			}
		}
	}
	for name, answers := range map[string][2]string{
		"RequestRefused":   {strconv.Itoa(requestRefusedStatus), requestRefusedCode},
		"AnswerNotWritten": {strconv.Itoa(answerNotWrittenStatus), answerNotWrittenCode},
	} {
		if declared[name] != answers {
			t.Errorf("%s declares status and code %v but answers %v", name, declared[name], answers)
		}
	}
}

func written(t *testing.T, answer Answer) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	Write(recorder, answer)
	return recorder.Code, recorder.Body.String()
}

func TestAnAnswerIsOneObjectOfItsTagCodeAndFields(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		answer Answer
		status int
		body   string
	}{
		{answer: Succeeded(http.StatusAccepted, "Queued", mo.Ok([]byte(`{"reference":"r1"}`))), status: http.StatusAccepted, body: `{"_tag":"Queued","reference":"r1"}`},
		{answer: Succeeded(http.StatusOK, "Empty", mo.Ok([]byte(`{}`))), status: http.StatusOK, body: `{"_tag":"Empty"}`},
		{
			answer: Failed(http.StatusNotFound, "Missing", "AB-1001", mo.Ok([]byte(`{"id":7}`))), status: http.StatusNotFound,
			body: `{"_tag":"Missing","code":"AB-1001","id":7}`,
		},
		{
			answer: Succeeded(http.StatusOK, "Listed", mo.Ok([]byte(`[1,2]`))), status: http.StatusInternalServerError,
			body: `{"_tag":"AnswerNotWritten","code":"NC-1002","answer":"Listed","reason":"nocontract: the answer Listed is not written as a JSON object"}`,
		},
		{answer: NoOutcome(), status: http.StatusInternalServerError, body: `{"_tag":"AnswerNotWritten","code":"NC-1002","answer":"","reason":"the route answered no outcome"}`},
	} {
		status, body := written(t, each.answer)
		if status != each.status || body != each.body {
			t.Errorf("wrote %d %s; want %d %s", status, body, each.status, each.body)
		}
		if !json.Valid([]byte(body)) {
			t.Errorf("wrote %s, which is not JSON", body)
		}
	}
}
