package nocontract

import (
	"cmp"
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"
	"golang.org/x/sync/errgroup"
)

// Endpoint is one declared route as a generated server's table holds it: its method, which is
// only compared with a request's, the segments of its path between slashes, and the answer it
// gives a request. The generator builds the route table from checked declarations.
type Endpoint struct {
	Method string
	Path   []PathSegment
	Answer func(request *http.Request) Answer
}

// PathSegment is one part of a declared path between two slashes: text a request's path must hold
// there, or a parameter that takes whatever the request holds there.
//
//sumtype:decl
type PathSegment interface {
	isPathSegment()
	// parameters is how many parameters the segment is: where two declared paths match one
	// request, the one with fewer parameters at the first segment they differ takes it.
	parameters() int
}

// PathText is a segment a request's path must hold as Text.
//
//ergo:not-an-error
type PathText struct{ Text string }

// PathParameter is a segment that takes any text a request's path holds there, read as the
// parameter Name.
//
//ergo:not-an-error
type PathParameter struct{ Name string }

func (PathText) isPathSegment()      {}
func (PathParameter) isPathSegment() {}

func (PathText) parameters() int      { return 0 }
func (PathParameter) parameters() int { return 1 }

// declaredPath is a path as its //nocontract:route line writes it, each parameter as {name}.
func declaredPath(segments []PathSegment) string {
	written := []string{}
	for _, segment := range segments {
		switch each := segment.(type) {
		case PathText:
			written = append(written, each.Text)
		case PathParameter:
			written = append(written, "{"+each.Name+"}")
		case nil:
		}
	}
	return "/" + strings.Join(written, "/")
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
// path's routes do not declare, an OPTIONS request, or a path that is not valid escaping.
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

// respond answers one request to a generated server through its layers: the route its method and
// path match; 204 with Allow to an OPTIONS request for a declared path; MethodNotDeclared to
// another method on a declared path; RouteNotFound to a path no route declares; and RequestRefused
// to a path holding a segment that is not valid escaping. The runtime's private HTTP handler
// calls it with the generated API's binding and request logger.
func respond(writer http.ResponseWriter, request *http.Request, endpoints []Endpoint, layers []Layer, logger outcome.Option[*slog.Logger], clock func() time.Time, deadline outcome.Option[uint32], workers *errgroup.Group) {
	started := clock()
	matched := matchRequest(endpoints, request)
	// Worker headers are values, separate from the listener's live response header.
	headers := http.Header{}
	visit := Visit{request: request, route: visitedRoute(matched), header: headers}
	completion := completeRequest(matched, request.Context(), deadline, workers, func(requestContext context.Context) Answered {
		workerRequest := request.WithContext(requestContext)
		workerVisit := Visit{request: workerRequest, route: visit.route, header: headers}
		answer := sync.OnceValue(func() Answered { return answerMatched(matched, workerRequest) })
		throughLayers(layers, workerVisit, answer)
		return answer()
	})
	publish := func(answered Answered) {
		outcome.ForEachOption(logger, func(logger *slog.Logger) {
			logRequest(logger, visit, answered, clock().Sub(started))
		})
		answered.write(writer)
	}
	switch completed := completion.(type) {
	case requestCompleted:
		for name, values := range headers {
			writer.Header()[name] = slices.Clone(values)
		}
		publish(completed.answer)
	case *requestExpired:
		publish(Answered{response: answerResponse{answer: sent(NocontractAnswerSharedFailure(RequestDeadlineExceeded{Route: completed.route, TimeoutMilliseconds: completed.milliseconds}))}})
	case requestCancelled, nil:
		// net/http requires its abort signal to close without synthesizing an empty 200.
		panic(http.ErrAbortHandler)
	}
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

// pathUnreadable is a request whose path holds a segment that is not valid escaping, and why.
//
//ergo:not-an-error
type pathUnreadable struct {
	segment string
	reason  string
}

func (endpointMatched) isRequestMatch() {}
func (pathMatched) isRequestMatch()     {}
func (nothingMatched) isRequestMatch()  {}
func (pathUnreadable) isRequestMatch()  {}

// visitedRoute is the route a layer is told the request matched.
func visitedRoute(matched requestMatch) VisitedRoute {
	switch match := matched.(type) {
	case endpointMatched:
		return RouteMatched{Method: match.candidate.endpoint.Method, Path: declaredPath(match.candidate.endpoint.Path)}
	case pathMatched, nothingMatched, pathUnreadable, nil:
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
	case pathUnreadable:
		refused := NocontractAnswerSharedFailure(RequestRefused{Part: RequestRefusedPartPath, Name: match.segment, Reason: match.reason})
		return Answered{response: answerResponse{answer: sent(refused)}}
	case nothingMatched, nil:
	}
	return Answered{response: answerResponse{answer: sent(NocontractAnswerSharedFailure(RouteNotFound{Method: request.Method, Path: request.URL.Path}))}}
}

// pathValue is one parameter of a declared path and the request's text in its place.
type pathValue struct {
	name  string
	value string
}

// candidate is an endpoint whose path matches a request's, and the request's text in place of each
// of its parameters.
type candidate struct {
	endpoint Endpoint
	values   []pathValue
}

// matchRequest is the endpoint a request matches.
func matchRequest(endpoints []Endpoint, request *http.Request) requestMatch {
	switch read := readRequestPath(request.URL.EscapedPath()).(type) {
	case pathRead:
		return matchSegments(endpoints, request.Method, read.segments)
	case pathUnreadable:
		return read
	case nil:
	}
	return nothingMatched{}
}

// matchSegments is the endpoint a request's method and path segments match. Of the declared paths
// matching the segments, the one that comes first by precedence is the request's path, and its
// routes are matched by method.
func matchSegments(endpoints []Endpoint, method string, segments []string) requestMatch {
	candidates := []candidate{}
	for _, endpoint := range endpoints {
		candidateFor(endpoint, segments).ForEach(func(matched candidate) { candidates = append(candidates, matched) })
	}
	if len(candidates) == 0 {
		return nothingMatched{}
	}
	first := slices.MinFunc(candidates, precedence)
	allowed := []string{}
	for _, each := range candidates {
		if precedence(each, first) != 0 {
			continue
		}
		if each.endpoint.Method == method {
			return endpointMatched{candidate: each}
		}
		allowed = append(allowed, each.endpoint.Method)
	}
	slices.Sort(allowed)
	return pathMatched{allowed: slices.Compact(allowed)}
}

// precedence orders two candidates for one request's path, which hold as many segments: at the
// first segment where one holds text and the other a parameter, the one holding text comes first.
// Two candidates equal by precedence are one declared path.
func precedence(left, right candidate) int {
	for i, segment := range left.endpoint.Path {
		if order := cmp.Compare(segment.parameters(), right.endpoint.Path[i].parameters()); order != 0 {
			return order
		}
	}
	return 0
}

// requestPath is a request's path read into segments, or the segment that could not be read.
//
//sumtype:decl
type requestPath interface{ isRequestPath() }

// pathRead is a request's path split at each slash after the first, each segment unescaped.
//
//ergo:not-an-error
type pathRead struct{ segments []string }

func (pathRead) isRequestPath()       {}
func (pathUnreadable) isRequestPath() {}

// readRequestPath is a request's escaped path split at each slash after the first, each segment
// unescaped, or the first segment that is not valid escaping.
func readRequestPath(escaped string) requestPath {
	read := requestPath(pathRead{segments: []string{}})
	for segment := range strings.SplitSeq(strings.TrimPrefix(escaped, "/"), "/") {
		switch sofar := read.(type) {
		case pathRead:
			read = mo.Fold(mo.TupleToResult(url.PathUnescape(segment)), func(unescaped string) requestPath {
				return pathRead{segments: append(sofar.segments, unescaped)}
			}, func(cause error) requestPath { return pathUnreadable{segment: segment, reason: cause.Error()} })
		case pathUnreadable, nil:
		}
	}
	return read
}

// candidateFor is the endpoint as a candidate for a request's path segments, absent when its path
// does not match them: as many segments, each text segment equal, and each parameter not empty.
func candidateFor(endpoint Endpoint, segments []string) mo.Option[candidate] {
	if len(endpoint.Path) != len(segments) {
		return mo.None[candidate]()
	}
	values := []pathValue{}
	for i, declared := range endpoint.Path {
		switch segment := declared.(type) {
		case PathText:
			if segment.Text != segments[i] {
				return mo.None[candidate]()
			}
		case PathParameter:
			if segments[i] == "" {
				return mo.None[candidate]()
			}
			values = append(values, pathValue{name: segment.Name, value: segments[i]})
		case nil:
			return mo.None[candidate]()
		}
	}
	return mo.Some(candidate{endpoint: endpoint, values: values})
}
