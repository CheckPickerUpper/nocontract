package nocontract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/outcome"
)

func TestTheListenerLogsTheAnswerEachRequestReceives(t *testing.T) {
	output := outcome.Fold(outcome.TupleToResult(os.CreateTemp(t.TempDir(), "request-log-*.jsonl")), func(output *os.File) *os.File {
		return output
	}, func(cause error) *os.File { t.Fatal(cause); return &os.File{} })
	t.Cleanup(func() {
		outcome.ForEachOption(outcome.EmptyableToOption(output.Close()), func(cause error) { t.Error(cause) })
	})
	logger := slog.New(slog.NewJSONHandler(output, nil)).With(slog.String("application", "shop"))
	check := func(request *http.Request) nocontract.Decision[struct{}] {
		if request.Header.Get("Shopper") == "" {
			return nocontract.Refused[struct{}]{Refusal: struct{}{}}
		}
		return nocontract.Admitted[struct{}]{}
	}
	binding := nocontract.BindRoutes([]nocontract.Endpoint{{
		Method: http.MethodGet,
		Path:   []nocontract.PathSegment{nocontract.PathText{Text: "shoppers"}, nocontract.PathParameter{Name: "shopper_id"}},
		Answer: func(request *http.Request) nocontract.Answer {
			return nocontract.Decided(check(request), func(struct{}) nocontract.Answer {
				return nocontract.Failed(nocontract.Failure{Status: http.StatusUnauthorized, Tag: "ShopperRequired", Code: "SH-1101"}, struct{}{}, nocontract.AnswerMarshalers{})
			}, func() nocontract.Answer { return answeredWith("Shopper")(request) })
		},
	}}, []nocontract.Layer{})
	options := nocontract.WithRequestLogger(nocontract.ListenOn("127.0.0.1:0"), logger)
	listening := outcome.Fold(nocontract.Listen(t.Context(), options, binding), func(listening nocontract.ListeningServer) nocontract.ListeningServer {
		return listening
	}, func(cause error) nocontract.ListeningServer { t.Fatal(cause); return nocontract.ListeningServer{} })
	t.Cleanup(func() {
		outcome.Fold(listening.Stop(context.WithoutCancel(t.Context())), func(nocontract.ServerStopped) struct{} { return struct{}{} }, func(cause error) struct{} {
			t.Error(cause)
			return struct{}{}
		})
	})
	requests := []struct {
		name    string
		method  string
		path    string
		shopper string
		route   string
		status  int
		code    string
	}{
		{name: "success", method: http.MethodGet, path: "/shoppers/7", shopper: "shopper-7", route: "/shoppers/{shopper_id}", status: http.StatusOK, code: ""},
		{name: "guard refusal", method: http.MethodGet, path: "/shoppers/7", shopper: "", route: "/shoppers/{shopper_id}", status: http.StatusUnauthorized, code: `"SH-1101"`},
		{name: "unmatched path", method: http.MethodGet, path: "/missing", shopper: "", route: "/missing", status: http.StatusNotFound, code: `"NC-1069"`},
		{name: "undeclared method", method: http.MethodPost, path: "/shoppers/7", shopper: "", route: "/shoppers/7", status: http.StatusMethodNotAllowed, code: `"NC-1070"`},
		{name: "options", method: http.MethodOptions, path: "/shoppers/7", shopper: "", route: "/shoppers/7", status: http.StatusNoContent, code: ""},
	}
	for _, example := range requests {
		t.Run(example.name, func(t *testing.T) {
			requestContext, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			request := outcome.Fold(outcome.TupleToResult(http.NewRequestWithContext(requestContext, example.method, "http://"+listening.Address()+example.path, http.NoBody)), func(request *http.Request) *http.Request {
				return request
			}, func(cause error) *http.Request { t.Fatal(cause); return &http.Request{} })
			request.Header.Set("Shopper", example.shopper)
			response := outcome.Fold(outcome.TupleToResult(http.DefaultClient.Do(request)), func(response *http.Response) *http.Response {
				return response
			}, func(cause error) *http.Response { t.Fatal(cause); return &http.Response{} })
			defer func() {
				outcome.ForEachOption(outcome.EmptyableToOption(response.Body.Close()), func(cause error) { t.Error(cause) })
			}()
			if response.StatusCode != example.status {
				t.Errorf("HTTP status = %d; want %d", response.StatusCode, example.status)
			}
		})
	}
	contents := outcome.Fold(outcome.TupleToResult(os.ReadFile(output.Name())), func(contents []byte) []byte { return contents }, func(cause error) []byte {
		t.Fatal(cause)
		return []byte{}
	})
	decoder := json.NewDecoder(bytes.NewReader(contents))
	for _, example := range requests {
		entry := struct {
			Application string          `json:"application"`
			Route       string          `json:"route"`
			Method      string          `json:"method"`
			Status      int             `json:"status"`
			Code        json.RawMessage `json:"code"`
			DurationUS  json.RawMessage `json:"duration_us"`
		}{Application: "", Route: "", Method: "", Status: 0, Code: json.RawMessage{}, DurationUS: json.RawMessage{}}
		outcome.ForEachOption(outcome.EmptyableToOption(decoder.Decode(&entry)), func(cause error) { t.Fatalf("%s request log: %v", example.name, cause) })
		if entry.Application != "shop" || entry.Route != example.route || entry.Method != example.method || entry.Status != example.status || string(entry.Code) != example.code {
			t.Errorf("%s request log = %+v; want application shop, route %s, method %s, status %d, code %s", example.name, entry, example.route, example.method, example.status, example.code)
		}
		outcome.Fold(outcome.TupleToResult(strconv.ParseInt(string(entry.DurationUS), 10, 64)), func(duration int64) struct{} {
			if duration < 0 {
				t.Errorf("%s duration_us = %d; want a nonnegative duration", example.name, duration)
			}
			return struct{}{}
		}, func(cause error) struct{} {
			t.Errorf("%s duration_us = %s: %v", example.name, entry.DurationUS, cause)
			return struct{}{}
		})
	}
	entry := json.RawMessage{}
	outcome.FoldOption(outcome.EmptyableToOption(decoder.Decode(&entry)), func(cause error) struct{} {
		if !errors.Is(cause, io.EOF) {
			t.Errorf("reading after the request logs: %v", cause)
		}
		return struct{}{}
	}, func() struct{} {
		t.Errorf("unexpected request log: %s", entry)
		return struct{}{}
	})
}
