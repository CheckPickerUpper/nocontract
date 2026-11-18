package running_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/go/reading"
	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

// servingCommand is a command of the ledger project holding body after the imports every case
// uses, each of them used once so the command compiles.
func servingCommand(body string) string {
	return "package main\n\nimport (\n\t\"net/http\"\n\t\"net/http/httptest\"\n\n\t\"example.com/shop/server\"\n)\n\n" +
		"var api server.API\n\nvar _ http.Handler = api\n\nvar _ *httptest.ResponseRecorder\n\n" + body
}

// servingTest is a test file of the ledger project's command holding body.
func servingTest(body string) string {
	return "package main\n\nimport (\n\t\"net/http\"\n\t\"net/http/httptest\"\n\t\"testing\"\n)\n\n" +
		"func TestServing(t *testing.T) {\n" + body + "}\n"
}

// placeOf is where marker is first written in the file at path under directory, as a refusal
// names a place.
func placeOf(directory, path, content, marker string) assembling.Position {
	before, _, _ := strings.Cut(content, marker)
	line := strings.Count(before, "\n") + 1
	column := len(before) - strings.LastIndex(before, "\n")
	return assembling.Position{File: filepath.Join(directory, filepath.FromSlash(path)), Line: line, Column: column}
}

// Each way a file the project wrote can serve something other than the generated API, or make a
// handler of its own, is refused with its own code at the place it is written; serving the API
// is not.
func TestServingAnythingButTheGeneratedAPIIsRefusedWhereItIsWritten(t *testing.T) {
	t.Parallel()
	command := "cmd/ledger/main.go"
	for _, each := range []struct {
		name   string
		path   string
		body   string
		marker string
		want   reading.Problem
	}{
		{
			name: "a handler of another type served by ListenAndServe", path: command,
			body:   servingCommand("func main() { _ = http.ListenAndServe(\":0\", http.NotFoundHandler()) }\n"),
			marker: "http.NotFoundHandler()", want: reading.ValueServedNotAPI{Function: "net/http.ListenAndServe", Type: "net/http.Handler"},
		},
		{
			name: "a handler of another type served by httptest in a test", path: "cmd/ledger/main_test.go",
			body:   servingTest("\thttptest.NewServer(http.NotFoundHandler()).Close()\n"),
			marker: "http.NotFoundHandler()", want: reading.ValueServedNotAPI{Function: "net/http/httptest.NewServer", Type: "net/http.Handler"},
		},
		{
			name: "a serving function used without calling it", path: command,
			body:   servingCommand("func main() { _ = http.ListenAndServe }\n"),
			marker: "ListenAndServe", want: reading.ServingFunctionNotCalled{Function: "net/http.ListenAndServe"},
		},
		{
			name: "an http.Server given another handler", path: command,
			body:   servingCommand("func main() { _ = &http.Server{Handler: http.NotFoundHandler()} }\n"),
			marker: "http.NotFoundHandler()", want: reading.ServerHandlerNotAPI{Type: "net/http.Handler"},
		},
		{
			name: "an http.Server without a handler", path: command,
			body:   servingCommand("func main() { _ = &http.Server{Addr: \":0\"} }\n"),
			marker: "http.Server{", want: reading.ServerHandlerMissing{},
		},
		{
			name: "an http.Server's handler set to another handler", path: command,
			body:   servingCommand("func main() { (&http.Server{Handler: api}).Handler = http.NotFoundHandler() }\n"),
			marker: "(&http.Server", want: reading.ServerHandlerAssigned{Type: "net/http.Handler"},
		},
		{
			name: "a function made a handler", path: command,
			body:   servingCommand("func main() { _ = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}) }\n"),
			marker: "HandlerFunc", want: reading.HandlerFuncUsed{},
		},
		{
			name: "a type that declares ServeHTTP", path: command,
			body:   servingCommand("func main() {}\n\ntype wrapper struct{}\n\nfunc (wrapper) ServeHTTP(http.ResponseWriter, *http.Request) {}\n"),
			marker: "ServeHTTP", want: reading.ServeHTTPDeclared{Type: "example.com/shop/cmd/ledger.wrapper"},
		},
		{
			name: "a handler registered on a ServeMux", path: command,
			body:   servingCommand("func main() { http.NewServeMux().Handle(\"/\", api) }\n"),
			marker: "Handle(", want: reading.HandlerRegistered{Function: "(*net/http.ServeMux).Handle"},
		},
		{
			name: "a function registered on the default ServeMux", path: command,
			body:   servingCommand("func main() { http.HandleFunc(\"/\", func(http.ResponseWriter, *http.Request) {}) }\n"),
			marker: "HandleFunc", want: reading.HandlerRegistered{Function: "net/http.HandleFunc"},
		},
	} {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()
			directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
			if each.path != command {
				scratchproject.WriteFile(t, directory, command, []byte(servingCommand("func main() {}\n")))
			}
			scratchproject.WriteFile(t, directory, each.path, []byte(each.body))
			generated := running.Run(running.Request{Arguments: []string{"generate", "-project", filepath.Join(directory, "nocontract.json")}, Context: t.Context()})
			refused, isRefused := generated.(running.DeclarationsRefused)
			if !isRefused {
				t.Fatalf("generate answered %v; want %s refused", generated.Lines(), each.want)
			}
			want := placeOf(directory, each.path, each.body, each.marker)
			got := []string{}
			for _, refusal := range refused.Refusals {
				got = append(got, fmt.Sprintf("%s %#v at %s", refusal.Problem.ErrorCode(), refusal.Problem, refusal.Position))
			}
			if wantLine := fmt.Sprintf("%s %#v at %s", each.want.ErrorCode(), each.want, want); len(got) != 1 || got[0] != wantLine {
				t.Fatalf("generate refused %q; want [%s]", got, wantLine)
			}
		})
	}
}

// A server holding the generated API, set to it, or served by httptest, is not refused.
func TestServingTheGeneratedAPIIsNotRefused(t *testing.T) {
	t.Parallel()
	directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
	scratchproject.WriteFile(t, directory, "cmd/ledger/main.go", []byte(servingCommand("func main() {\n"+
		"\tserved := &http.Server{Handler: api}\n\tserved.Handler = api\n\t_ = served.ListenAndServe()\n\thttptest.NewServer(api).Close()\n}\n")))
	generated := running.Run(running.Request{Arguments: []string{"generate", "-project", filepath.Join(directory, "nocontract.json")}, Context: t.Context()})
	if _, isWritten := generated.(running.FilesWritten); !isWritten {
		t.Fatalf("generate answered %v; want files written", generated.Lines())
	}
}
