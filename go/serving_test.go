package nocontract_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/outcome"
)

// answeredWith is a route answering every request with tag, so a test can tell which route ran.
func answeredWith(tag string) func(*http.Request, nocontract.RequestBodyLimit) nocontract.Answer {
	return func(*http.Request, nocontract.RequestBodyLimit) nocontract.Answer {
		return nocontract.Succeeded(nocontract.Success{Status: http.StatusOK, Tag: tag}, struct{}{}, nocontract.AnswerMarshalers{})
	}
}

// A request both a text route and a parameter route match goes to the text route, whatever order
// the routes are declared in, and a request only the parameter route matches goes to it.
func TestATextSegmentTakesARequestBeforeAParameterAtTheSamePlace(t *testing.T) {
	t.Parallel()
	me := nocontract.Endpoint{Method: http.MethodGet, Path: []nocontract.PathSegment{nocontract.PathText{Text: "shoppers"}, nocontract.PathText{Text: "me"}}, Answer: answeredWith("Me")}
	one := nocontract.Endpoint{
		Method: http.MethodGet, Path: []nocontract.PathSegment{nocontract.PathText{Text: "shoppers"}, nocontract.PathParameter{Name: "shopper_id"}},
		Answer: answeredWith("Shopper"),
	}
	for _, each := range []struct {
		name      string
		endpoints []nocontract.Endpoint
		path      string
		body      string
	}{
		{name: "text route declared first", endpoints: []nocontract.Endpoint{me, one}, path: "/shoppers/me", body: `{"_tag":"Me"}`},
		{name: "parameter route declared first", endpoints: []nocontract.Endpoint{one, me}, path: "/shoppers/me", body: `{"_tag":"Me"}`},
		{name: "a path only the parameter matches", endpoints: []nocontract.Endpoint{me, one}, path: "/shoppers/7", body: `{"_tag":"Shopper"}`},
	} {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()
			outcome.Fold(nocontract.Listen(t.Context(), nocontract.ListenOn("127.0.0.1:0"), nocontract.BindRoutes(each.endpoints, []nocontract.Layer{})), func(server nocontract.ListeningServer) struct{} {
				t.Cleanup(func() {
					outcome.Fold(server.Stop(context.WithoutCancel(t.Context())), func(nocontract.ServerStopped) struct{} { return struct{}{} }, func(cause error) struct{} { t.Error(cause); return struct{}{} })
				})
				outcome.Fold(outcome.TupleToResult(http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+server.Address()+each.path, http.NoBody)), func(request *http.Request) struct{} {
					return outcome.Fold(outcome.TupleToResult(http.DefaultClient.Do(request)), func(response *http.Response) struct{} {
						defer func() {
							outcome.ForEachOption(outcome.EmptyableToOption(response.Body.Close()), func(cause error) { t.Error(cause) })
						}()
						return outcome.Fold(outcome.TupleToResult(io.ReadAll(response.Body)), func(body []byte) struct{} {
							if response.StatusCode != http.StatusOK || string(body) != each.body {
								t.Errorf("GET %s was answered %d %s; want 200 %s", each.path, response.StatusCode, body, each.body)
							}
							return struct{}{}
						}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
					}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
				}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
				return struct{}{}
			}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
		})
	}
}
