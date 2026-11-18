package nocontract_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samber/mo"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

// answeredWith is a route answering every request with tag, so a test can tell which route ran.
func answeredWith(tag string) func(*http.Request) nocontract.Answer {
	return func(*http.Request) nocontract.Answer {
		return nocontract.Succeeded(nocontract.Success{Status: http.StatusOK, Tag: tag, Fields: mo.Ok([]byte(`{}`))})
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
			recorder := httptest.NewRecorder()
			nocontract.Respond(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, each.path, http.NoBody), each.endpoints, []nocontract.Layer{})
			if recorder.Code != http.StatusOK || recorder.Body.String() != each.body {
				t.Fatalf("GET %s was answered %d %s; want 200 %s", each.path, recorder.Code, recorder.Body.String(), each.body)
			}
		})
	}
}
