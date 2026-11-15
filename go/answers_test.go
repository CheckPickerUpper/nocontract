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
// their declarations carry, which is what the generator writes into the TypeScript. Their codes
// have one owner, ErGo's generated ErrorCode.
func TestSharedFailuresAnswerWhatTheirDirectivesDeclare(t *testing.T) {
	t.Parallel()
	declarations := mo.Fold(mo.TupleToResult(parser.ParseFile(token.NewFileSet(), "shared_failures.go", nil, parser.ParseComments)),
		func(parsed *ast.File) []ast.Decl { return parsed.Decls }, func(cause error) []ast.Decl {
			t.Fatal(cause)
			return []ast.Decl{}
		})
	declared := map[string]string{}
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
				if status, isStatus := strings.CutPrefix(line.Text, "//nocontract:status "); isStatus {
					declared[name] = status
				}
			}
		}
	}
	for name, status := range map[string]string{
		"RequestRefused":   strconv.Itoa(requestRefusedStatus),
		"AnswerNotWritten": strconv.Itoa(answerNotWrittenStatus),
	} {
		if declared[name] != status {
			t.Errorf("%s declares status %s but answers %s", name, declared[name], status)
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
		{answer: Succeeded(Success{Status: http.StatusAccepted, Tag: "Queued", Fields: mo.Ok([]byte(`{"reference":"r1"}`))}), status: http.StatusAccepted, body: `{"_tag":"Queued","reference":"r1"}`},
		{answer: Succeeded(Success{Status: http.StatusOK, Tag: "Empty", Fields: mo.Ok([]byte(`{}`))}), status: http.StatusOK, body: `{"_tag":"Empty"}`},
		{
			answer: Failed(Failure{Status: http.StatusNotFound, Tag: "Missing", Code: "AB-1001", Fields: mo.Ok([]byte(`{"id":7}`))}), status: http.StatusNotFound,
			body: `{"_tag":"Missing","code":"AB-1001","id":7}`,
		},
		{
			answer: Succeeded(Success{Status: http.StatusOK, Tag: "Listed", Fields: mo.Ok([]byte(`[1,2]`))}), status: http.StatusInternalServerError,
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
