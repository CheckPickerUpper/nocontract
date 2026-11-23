package nocontract_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/outcome"
)

func TestParentCancellationClosesTheRequestWithoutPublishingAnImplicitAnswer(t *testing.T) {
	serverContext, cancel := context.WithCancelCause(t.Context())
	defer cancel(context.Canceled)
	release, observed := make(chan struct{}), make(chan error, 1)
	options := outcome.Fold(nocontract.WithRequestDeadline(nocontract.ListenOn("127.0.0.1:0"), 1000), func(options nocontract.ListenOptions) nocontract.ListenOptions { return options }, func(cause error) nocontract.ListenOptions { t.Fatal(cause); return nocontract.ListenOn("") })
	bound := nocontract.BindRoutes([]nocontract.Endpoint{{Method: http.MethodGet, Path: []nocontract.PathSegment{nocontract.PathText{Text: "cancel"}}, Answer: func(request *http.Request) nocontract.Answer {
		cancel(context.Canceled)
		<-request.Context().Done()
		observed <- context.Cause(request.Context())
		<-release
		return nocontract.Succeeded(nocontract.Success{Status: 200, Tag: "WorkFinished"}, struct{}{}, nocontract.AnswerMarshalers{})
	}}}, []nocontract.Layer{})
	listening := outcome.Fold(nocontract.Listen(serverContext, options, bound), func(server nocontract.ListeningServer) nocontract.ListeningServer { return server }, func(cause error) nocontract.ListeningServer { t.Fatal(cause); return nocontract.ListeningServer{} })
	t.Cleanup(func() {
		outcome.Fold(listening.Stop(context.WithoutCancel(t.Context())), func(nocontract.ServerStopped) struct{} { return struct{}{} }, func(cause error) struct{} { t.Error(cause); return struct{}{} })
	})
	defer close(release)
	request := outcome.Fold(outcome.TupleToResult(http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+listening.Address()+"/cancel", http.NoBody)), func(request *http.Request) *http.Request { return request }, func(cause error) *http.Request { t.Fatal(cause); return &http.Request{} })
	outcome.Fold(outcome.TupleToResult(http.DefaultClient.Do(request)), func(response *http.Response) struct{} {
		defer func() {
			outcome.ForEachOption(outcome.EmptyableToOption(response.Body.Close()), func(cause error) { t.Error(cause) })
		}()
		t.Errorf("the cancelled request published status %d; want a closed transport", response.StatusCode)
		return struct{}{}
	}, func(cause error) struct{} {
		if !errors.Is(cause, io.EOF) {
			t.Errorf("the cancelled request ended with %v; want the server to close without an answer", cause)
		}
		return struct{}{}
	})
	if cause := <-observed; !errors.Is(cause, context.Canceled) {
		t.Errorf("the handler observed %v; want the original parent cancellation", cause)
	}
}
