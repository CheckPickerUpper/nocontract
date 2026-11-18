package running_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/go/reading"
	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

// placeOf is where marker is first written in content, the file at path under directory, as a
// refusal names a place.
func placeOf(directory, path, content, marker string) assembling.Position {
	before, _, _ := strings.Cut(content, marker)
	line := strings.Count(before, "\n") + 1
	column := len(before) - strings.LastIndex(before, "\n")
	return assembling.Position{File: filepath.Join(directory, filepath.FromSlash(path)), Line: line, Column: column}
}

// servingFile is one file of the ledger project's command: what it holds, and where and with what
// it is refused, when it is.
type servingFile struct {
	path    string
	content string
	refused []servingRefusal
}

// servingRefusal is a refusal a file is expected to get: the problem, and the text written where
// it is.
type servingRefusal struct {
	marker  string
	problem reading.Problem
}

// servingFiles is the ledger project's command: a file for each way to serve something other than
// the generated API or make a handler, and a file serving the API, which nothing refuses.
func servingFiles() []servingFile {
	return []servingFile{
		{path: "cmd/ledger/main.go", content: "package main\n\nfunc main() {}\n", refused: []servingRefusal{}},
		{
			path:    "cmd/ledger/served_by_listen_and_serve.go",
			content: "package main\n\nimport \"net/http\"\n\nfunc serveByListenAndServe() { _ = http.ListenAndServe(\":0\", http.NotFoundHandler()) }\n",
			refused: []servingRefusal{{marker: "http.NotFoundHandler()", problem: reading.ValueServedNotAPI{Function: "net/http.ListenAndServe", Type: "net/http.Handler"}}},
		},
		{
			path: "cmd/ledger/main_test.go",
			content: "package main\n\nimport (\n\t\"net/http\"\n\t\"net/http/httptest\"\n\t\"testing\"\n)\n\n" +
				"func TestServing(t *testing.T) { httptest.NewServer(http.NotFoundHandler()).Close() }\n",
			refused: []servingRefusal{{marker: "http.NotFoundHandler()", problem: reading.ValueServedNotAPI{Function: "net/http/httptest.NewServer", Type: "net/http.Handler"}}},
		},
		{
			path:    "cmd/ledger/serving_function_uncalled.go",
			content: "package main\n\nimport \"net/http\"\n\nfunc keepServingFunction() { _ = http.ListenAndServe }\n",
			refused: []servingRefusal{{marker: "ListenAndServe", problem: reading.ServingFunctionNotCalled{Function: "net/http.ListenAndServe"}}},
		},
		{
			path:    "cmd/ledger/server_given_another_handler.go",
			content: "package main\n\nimport \"net/http\"\n\nfunc giveAnotherHandler() { _ = &http.Server{Handler: http.NotFoundHandler()} }\n",
			refused: []servingRefusal{{marker: "http.NotFoundHandler()", problem: reading.ServerHandlerNotAPI{Type: "net/http.Handler"}}},
		},
		{
			path:    "cmd/ledger/server_without_handler.go",
			content: "package main\n\nimport \"net/http\"\n\nfunc leaveHandlerOut() { _ = &http.Server{Addr: \":0\"} }\n",
			refused: []servingRefusal{{marker: "http.Server{", problem: reading.ServerHandlerMissing{}}},
		},
		{
			path: "cmd/ledger/server_handler_set.go",
			content: "package main\n\nimport (\n\t\"net/http\"\n\n\t\"example.com/shop/server\"\n)\n\n" +
				"func setAnotherHandler() { (&http.Server{Handler: server.API{}}).Handler = http.NotFoundHandler() }\n",
			refused: []servingRefusal{{marker: "(&http.Server", problem: reading.ServerHandlerAssigned{Type: "net/http.Handler"}}},
		},
		{
			path:    "cmd/ledger/handler_func.go",
			content: "package main\n\nimport \"net/http\"\n\nfunc makeHandler() { _ = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}) }\n",
			refused: []servingRefusal{{marker: "HandlerFunc", problem: reading.HandlerFuncUsed{}}},
		},
		{
			path:    "cmd/ledger/serve_http_declared.go",
			content: "package main\n\nimport \"net/http\"\n\ntype wrapper struct{}\n\nfunc (wrapper) ServeHTTP(http.ResponseWriter, *http.Request) {}\n",
			refused: []servingRefusal{{marker: "ServeHTTP", problem: reading.ServeHTTPDeclared{Type: "example.com/shop/cmd/ledger.wrapper"}}},
		},
		{
			path: "cmd/ledger/mux_handle.go",
			content: "package main\n\nimport (\n\t\"net/http\"\n\n\t\"example.com/shop/server\"\n)\n\n" +
				"func registerAPI() { http.NewServeMux().Handle(\"/\", server.API{}) }\n",
			refused: []servingRefusal{{marker: "Handle(", problem: reading.HandlerRegistered{Function: "(*net/http.ServeMux).Handle"}}},
		},
		{
			path:    "cmd/ledger/default_mux_handle_func.go",
			content: "package main\n\nimport \"net/http\"\n\nfunc registerFunction() { http.HandleFunc(\"/\", func(http.ResponseWriter, *http.Request) {}) }\n",
			refused: []servingRefusal{{marker: "HandleFunc", problem: reading.HandlerRegistered{Function: "net/http.HandleFunc"}}},
		},
		{
			path: "cmd/ledger/served_api.go",
			content: "package main\n\nimport (\n\t\"net/http\"\n\t\"net/http/httptest\"\n\n\t\"example.com/shop/server\"\n)\n\n" +
				"func serveAPI(api server.API) {\n\tserved := &http.Server{Handler: api}\n\tserved.Handler = api\n\t_ = served.ListenAndServe()\n\thttptest.NewServer(api).Close()\n}\n",
			refused: []servingRefusal{},
		},
	}
}

// Each way a file the project wrote, tests included, can serve something other than the generated
// API, or make a handler of its own, is refused with its own problem at the place it is written;
// a file serving the API, set on a server, assigned to its Handler and handed to httptest, is not.
func TestServingAnythingButTheGeneratedAPIIsRefusedWhereItIsWritten(t *testing.T) {
	t.Parallel()
	directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
	want := []string{}
	for _, file := range servingFiles() {
		scratchproject.WriteFile(t, directory, file.path, []byte(file.content))
		for _, refusal := range file.refused {
			place := placeOf(directory, file.path, file.content, refusal.marker)
			want = append(want, fmt.Sprintf("%s %#v at %s", refusal.problem.ErrorCode(), refusal.problem, place))
		}
	}
	generated := running.Run(running.Request{Arguments: []string{"generate", "-project", filepath.Join(directory, "nocontract.json")}, Context: t.Context()})
	refused, isRefused := generated.(running.DeclarationsRefused)
	if !isRefused {
		t.Fatalf("generate answered %v; want the serving files refused", generated.Lines())
	}
	got := []string{}
	for _, refusal := range refused.Refusals {
		got = append(got, fmt.Sprintf("%s %#v at %s", refusal.Problem.ErrorCode(), refusal.Problem, refusal.Position))
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("generate refused\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
