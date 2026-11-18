package nocontract

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/samber/mo"
)

// Endpoint is one declared route as a generated server's table holds it: its method, its path as
// its //nocontract:route line declares it, and the answer it gives a request. Only generated code
// builds one, and it serves nothing on its own.
type Endpoint struct {
	Method string
	Path   string
	Answer func(request *http.Request) Answer
}

// Layer runs around every request a server answers, such as to log it, count it, tag it with a
// request id or add CORS headers. It is handed the request, the route it matched and the response
// header, and next, which runs the route once however often it is called and returns what the
// route answered. A layer cannot answer in the route's place: the server sends the route's own
// answer whatever the layer returns, so a layer returns what next returned. Layers run in the order
// given, the first outermost.
type Layer func(visit Visit, next func() Answered) Answered

// Visit is one request as a layer sees it: what it asked for, the route it matched, and the header
// the server answers it with, which the layer may set.
type Visit struct {
	request *http.Request
	route   VisitedRoute
	header  http.Header
}

// Method is the request's method.
func (visit Visit) Method() string { return visit.request.Method }

// URL is a copy of the request's URL.
func (visit Visit) URL() url.URL { return *visit.request.URL }

// RequestHeader is a copy of the request's header.
func (visit Visit) RequestHeader() http.Header { return visit.request.Header.Clone() }

// RemoteAddr is the network address the request came from.
func (visit Visit) RemoteAddr() string { return visit.request.RemoteAddr }

// Context is the request's context.
func (visit Visit) Context() context.Context { return visit.request.Context() }

// Route is the declared route the request matched, or none.
func (visit Visit) Route() VisitedRoute { return visit.route }

// ResponseHeader is the header the server answers the request with. A header a layer sets here is
// sent with the answer.
func (visit Visit) ResponseHeader() http.Header { return visit.header }

// VisitedRoute is the declared route a request matched, or none.
//
//sumtype:decl
type VisitedRoute interface{ isVisitedRoute() }

// RouteMatched is the route a request matched: its method and its path as declared, such as
// "/shoppers/{shopper_id}/balance".
//
//ergo:not-an-error
type RouteMatched struct {
	Method string
	Path   string
}

// NoRouteMatched is a request no declared route takes: a path no route declares, a method the
// path's routes do not declare, or an OPTIONS request.
//
//ergo:not-an-error
type NoRouteMatched struct{}

func (RouteMatched) isVisitedRoute()   {}
func (NoRouteMatched) isVisitedRoute() {}

// Answered is what the server answered a request with. Only the server builds it, from the answer
// it sends, so a layer reads it and cannot make one up.
type Answered struct {
	response response
}

// Status is the answer's HTTP status.
func (answered Answered) Status() int {
	switch sending := answered.response.(type) {
	case answerResponse:
		return sending.answer.status
	case methodResponse:
		return sending.answer.status
	case optionsResponse:
		return http.StatusNoContent
	case nil:
	}
	return 0
}

// Tag is the answer's _tag, absent for an answer to an OPTIONS request, which has no body.
func (answered Answered) Tag() mo.Option[string] {
	switch sending := answered.response.(type) {
	case answerResponse:
		return mo.Some(sending.answer.tag)
	case methodResponse:
		return mo.Some(sending.answer.tag)
	case optionsResponse, nil:
	}
	return mo.None[string]()
}

// Code is the permanent code of a failure the answer carries, absent for a success and for an
// answer to an OPTIONS request.
func (answered Answered) Code() mo.Option[string] {
	switch sending := answered.response.(type) {
	case answerResponse:
		return sending.answer.code
	case methodResponse:
		return sending.answer.code
	case optionsResponse, nil:
	}
	return mo.None[string]()
}

// write sends the answer.
func (answered Answered) write(writer http.ResponseWriter) {
	switch sending := answered.response.(type) {
	case answerResponse:
		sending.answer.write(writer)
	case methodResponse:
		writer.Header().Set("Allow", strings.Join(sending.allowed, ", "))
		sending.answer.write(writer)
	case optionsResponse:
		writer.Header().Set("Allow", strings.Join(sending.allowed, ", "))
		writer.WriteHeader(http.StatusNoContent)
	case nil:
	}
}

// response is how a request is answered.
//
//sumtype:decl
type response interface{ isResponse() }

// answerResponse is an answer sent as it is: a route's own, or RouteNotFound.
//
//ergo:not-an-error
type answerResponse struct{ answer sentAnswer }

// methodResponse is MethodNotDeclared, sent with the methods the path's routes declare.
//
//ergo:not-an-error
type methodResponse struct {
	answer  sentAnswer
	allowed []string
}

// optionsResponse is the answer to an OPTIONS request for a declared path: no body, and the
// methods the path's routes declare.
//
//ergo:not-an-error
type optionsResponse struct{ allowed []string }

func (answerResponse) isResponse()  {}
func (methodResponse) isResponse()  {}
func (optionsResponse) isResponse() {}

// Respond answers one request to a generated server through its layers: the route its method and
// path match; 204 with Allow to an OPTIONS request for a declared path; MethodNotDeclared to
// another method on a declared path; and RouteNotFound to a path no route declares. Only the
// generated API's ServeHTTP calls it.
func Respond(writer http.ResponseWriter, request *http.Request, endpoints []Endpoint, layers []Layer) {
	matched := matchRequest(endpoints, request)
	answer := sync.OnceValue(func() Answered { return answerMatched(matched, request) })
	throughLayers(layers, Visit{request: request, route: visitedRoute(matched), header: writer.Header()}, answer)
	answer().write(writer)
}

// throughLayers runs each layer around the next, the first outermost, each next running what is
// inside it once.
func throughLayers(layers []Layer, visit Visit, answer func() Answered) {
	if len(layers) == 0 {
		return
	}
	next := sync.OnceValue(func() Answered {
		throughLayers(layers[1:], visit, answer)
		return answer()
	})
	// What the layer returns is not what is sent: the route's own answer is.
	_ = layers[0](visit, next)
}

// requestMatch is which declared route a request matched.
//
//sumtype:decl
type requestMatch interface{ isRequestMatch() }

// endpointMatched is the endpoint whose method and path the request matched, with the path's
// values.
//
//ergo:not-an-error
type endpointMatched struct{ candidate candidate }

// pathMatched is a request to a declared path whose routes do not declare its method, and the
// methods they declare, sorted.
//
//ergo:not-an-error
type pathMatched struct{ allowed []string }

// nothingMatched is a request to a path no route declares.
//
//ergo:not-an-error
type nothingMatched struct{}

func (endpointMatched) isRequestMatch() {}
func (pathMatched) isRequestMatch()     {}
func (nothingMatched) isRequestMatch()  {}

// visitedRoute is the route a layer is told the request matched.
func visitedRoute(matched requestMatch) VisitedRoute {
	switch match := matched.(type) {
	case endpointMatched:
		return RouteMatched{Method: match.candidate.endpoint.Method, Path: match.candidate.endpoint.Path}
	case pathMatched, nothingMatched, nil:
	}
	return NoRouteMatched{}
}

// answerMatched is the answer to a request: its route's, with the path's values set on the request,
// or the server's own.
func answerMatched(matched requestMatch, request *http.Request) Answered {
	switch match := matched.(type) {
	case endpointMatched:
		for _, value := range match.candidate.values {
			request.SetPathValue(value.name, value.value)
		}
		return Answered{response: answerResponse{answer: sent(match.candidate.endpoint.Answer(request))}}
	case pathMatched:
		if request.Method == http.MethodOptions {
			return Answered{response: optionsResponse(match)}
		}
		refused := NocontractAnswerSharedFailure(MethodNotDeclared{Method: request.Method, Allowed: match.allowed})
		return Answered{response: methodResponse{answer: sent(refused), allowed: match.allowed}}
	case nothingMatched, nil:
	}
	return Answered{response: answerResponse{answer: sent(NocontractAnswerSharedFailure(RouteNotFound{Method: request.Method, Path: request.URL.Path}))}}
}

// pathValue is one {name} segment of a declared path and the request's segment in its place.
type pathValue struct {
	name  string
	value string
}

// candidate is an endpoint whose path matches a request's: the endpoint, which of its segments are
// text ('0') and which are parameters ('1'), and the parameters' values.
type candidate struct {
	endpoint Endpoint
	shape    string
	values   []pathValue
}

// matchRequest is the endpoint a request matches. Of the declared paths matching the request's
// path, the one with text where another has a parameter, at the first segment they differ, is the
// request's path; its routes are matched by method.
func matchRequest(endpoints []Endpoint, request *http.Request) requestMatch {
	candidates := mo.Fold(requestSegments(request.URL.EscapedPath()), func(segments []string) []candidate {
		found := []candidate{}
		for _, endpoint := range endpoints {
			candidateFor(endpoint, segments).ForEach(func(matched candidate) { found = append(found, matched) })
		}
		return found
	}, func(error) []candidate { return []candidate{} })
	if len(candidates) == 0 {
		return nothingMatched{}
	}
	// '0' sorts before '1', so the least shape has text first where the shapes differ.
	shape := slices.MinFunc(candidates, func(left, right candidate) int { return cmp.Compare(left.shape, right.shape) }).shape
	allowed := []string{}
	for _, each := range candidates {
		if each.shape != shape {
			continue
		}
		if each.endpoint.Method == request.Method {
			return endpointMatched{candidate: each}
		}
		allowed = append(allowed, each.endpoint.Method)
	}
	slices.Sort(allowed)
	return pathMatched{allowed: slices.Compact(allowed)}
}

// requestSegments is a request's escaped path split at each slash, each segment unescaped, absent
// when a segment is not valid escaping.
func requestSegments(escaped string) mo.Option[[]string] {
	segments := mo.Some([]string{})
	for segment := range strings.SplitSeq(escaped, "/") {
		segments = mo.Fold(segments, func(read []string) mo.Option[[]string] {
			return mo.Fold(mo.TupleToResult(url.PathUnescape(segment)), func(unescaped string) mo.Option[[]string] {
				return mo.Some(append(read, unescaped))
			}, func(error) mo.Option[[]string] { return mo.None[[]string]() })
		}, func(error) mo.Option[[]string] { return mo.None[[]string]() })
	}
	return segments
}

// candidateFor is the endpoint as a candidate for a request's path segments, absent when its path
// does not match them: each text segment equal, and each {name} segment not empty.
func candidateFor(endpoint Endpoint, segments []string) mo.Option[candidate] {
	declared := strings.Split(endpoint.Path, "/")
	if len(declared) != len(segments) {
		return mo.None[candidate]()
	}
	shape, values := &strings.Builder{}, []pathValue{}
	for i, pattern := range declared {
		segment := segments[i]
		inner, opens := strings.CutPrefix(pattern, "{")
		name, closes := strings.CutSuffix(inner, "}")
		switch {
		case opens && closes:
			if segment == "" {
				return mo.None[candidate]()
			}
			shape.WriteString("1")
			values = append(values, pathValue{name: name, value: segment})
		case pattern == segment:
			shape.WriteString("0")
		default:
			return mo.None[candidate]()
		}
	}
	return mo.Some(candidate{endpoint: endpoint, shape: shape.String(), values: values})
}
