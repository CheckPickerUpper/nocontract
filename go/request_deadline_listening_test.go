package nocontract_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/outcome"
)

func TestAListenerAnswersAtItsDeadlineWhileItsCancelledHandlerIsStillRunning(t *testing.T) {
	release, cancelled, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	options := outcome.Fold(nocontract.WithRequestDeadline(nocontract.ListenOn("127.0.0.1:0"), 10), func(options nocontract.ListenOptions) nocontract.ListenOptions {
		return options
	}, func(cause error) nocontract.ListenOptions { t.Fatal(cause); return nocontract.ListenOn("") })
	options = nocontract.WithRequestLogger(options, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	endpoints := []nocontract.Endpoint{{
		Method: http.MethodGet,
		Path:   []nocontract.PathSegment{nocontract.PathText{Text: "work"}, nocontract.PathParameter{Name: "kind"}},
		Answer: func(request *http.Request) nocontract.Answer {
			if request.PathValue("kind") == "slow" {
				<-request.Context().Done()
				close(cancelled)
				<-release
				close(finished)
			}
			return nocontract.Succeeded(nocontract.Success{Status: 200, Tag: "WorkFinished"}, struct{}{}, nocontract.AnswerMarshalers{})
		},
	}}
	listening := outcome.Fold(nocontract.Listen(t.Context(), options, nocontract.BindRoutes(endpoints, []nocontract.Layer{})), func(server nocontract.ListeningServer) nocontract.ListeningServer {
		return server
	}, func(cause error) nocontract.ListeningServer { t.Fatal(cause); return nocontract.ListeningServer{} })
	t.Cleanup(func() {
		outcome.Fold(listening.Stop(context.WithoutCancel(t.Context())), func(nocontract.ServerStopped) struct{} { return struct{}{} }, func(cause error) struct{} { t.Error(cause); return struct{}{} })
	})
	defer close(release)
	client := *http.DefaultClient
	client.Timeout = time.Second
	for _, request := range []struct {
		path   string
		status int
		body   string
	}{
		{path: "/work/fast", status: 200, body: `{"_tag":"WorkFinished"}`},
		{path: "/work/slow", status: 503, body: `{"_tag":"RequestDeadlineExceeded","code":"NC-1170","route":"/work/{kind}","timeout_milliseconds":10}`},
	} {
		built := outcome.Fold(outcome.TupleToResult(http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+listening.Address()+request.path, http.NoBody)), func(request *http.Request) *http.Request { return request }, func(cause error) *http.Request { t.Fatal(cause); return &http.Request{} })
		response := outcome.Fold(outcome.TupleToResult(client.Do(built)), func(response *http.Response) *http.Response { return response }, func(cause error) *http.Response { t.Fatal(cause); return &http.Response{} })
		body := outcome.Fold(outcome.TupleToResult(io.ReadAll(response.Body)), func(body []byte) []byte { return body }, func(cause error) []byte { t.Fatal(cause); return []byte{} })
		outcome.ForEachOption(outcome.EmptyableToOption(response.Body.Close()), func(cause error) { t.Fatal(cause) })
		if response.StatusCode != request.status || string(body) != request.body {
			t.Fatalf("GET %s = %d %s; want %d %s", request.path, response.StatusCode, body, request.status, request.body)
		}
	}
	<-cancelled
	select {
	case <-finished:
		t.Fatal("the slow handler finished before the timeout answer arrived")
	default:
	}
}
