package assembling

import (
	"cmp"
	"fmt"
	"slices"

	ergo "github.com/CheckPickerUpper/ErGo"

	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/nocontract/messages"
)

// Position is where a declaration is written: its file, line and column.
type Position struct {
	File   string
	Line   int
	Column int
}

// String is the position as a compiler writes one.
func (position Position) String() string {
	return fmt.Sprintf("%s:%d:%d", position.File, position.Line, position.Column)
}

// Refusal is one declaration the API cannot hold, where it is written and why.
type Refusal struct {
	Position Position
	Problem  Problem
}

// Problem is why a declaration is refused, as ErGo describes a failure: its permanent code, its
// message in a language, and the Go errors it holds. Each backend's reader refuses with problems of
// its own beside the ones every route shares.
type Problem interface {
	ErrorCode() ergo.Code
	Report(language messages.Language) ergo.Report
	Causes() []error
}

// sortedRefusals is every refusal once, in source order: a declaration refused for one reason by
// several routes is refused once.
func sortedRefusals(refusals []Refusal) []Refusal {
	kept := keyed.FirstOfEach(refusals, func(refusal Refusal) Refusal { return refusal })
	slices.SortStableFunc(kept, func(left, right Refusal) int {
		return cmp.Or(cmp.Compare(left.Position.File, right.Position.File), cmp.Compare(left.Position.Line, right.Position.Line))
	})
	return kept
}

// NameRepeated is a name two declarations would both take in the client, and what took it first.
//
//ergo:code NC-1018
//ergo:message The name {Name} is already taken by {First}.
type NameRepeated struct {
	Name  string
	First string
}

// PathParameterUndeclared is a path segment no input field reads.
//
//ergo:code NC-1022
//ergo:message No input field reads the path segment named {Parameter}.
type PathParameterUndeclared struct{ Parameter string }

// PathFieldUnused is an input field reading a path segment the path does not have.
//
//ergo:code NC-1023
//ergo:message An input field reads the path segment named {Parameter}, which the path does not have.
type PathFieldUnused struct{ Parameter string }

// PathSegmentUnsupported is a path segment with no Effect HttpApi form: a {name...} or {$}
// wildcard, or a brace that does not enclose the whole segment.
//
//ergo:code NC-1024
//ergo:message The path segment {Segment} has no Effect HttpApi form.
type PathSegmentUnsupported struct{ Segment string }

// BodyWithoutRequestBody is a route reading a body although its method's request carries none.
//
//ergo:code NC-1025
//ergo:message The route {Endpoint} reads a body, which a {Method} request does not carry.
type BodyWithoutRequestBody struct {
	Endpoint string
	Method   string
}

// RouteRepeated is a method and path two routes both serve, and where the first serves it.
//
//ergo:code NC-1026
//ergo:message {Pattern} is already served by the route at {First}.
type RouteRepeated struct {
	Pattern string
	First   string
}

// EndpointWithoutSuccess is a route whose outcome has no member answered with a success status.
//
//ergo:code NC-1028
//ergo:message The route {Endpoint} has no outcome member answered with a success status.
type EndpointWithoutSuccess struct{ Endpoint string }

// FieldNameReserved is a member field whose JSON name is one every answer starts with.
//
//ergo:code NC-1033
//ergo:message The member {Member}'s JSON name "{Field}" is taken by the tag and code every answer starts with.
type FieldNameReserved struct {
	Member string
	Field  string
}

// EndpointNameRepeated is an endpoint name two routes of one group both use, and where the first
// uses it.
//
//ergo:code NC-1050
//ergo:message The endpoint name {Name} is already taken in this group, at {First}.
type EndpointNameRepeated struct {
	Name  string
	First string
}

// MemberUndeclared is a member a route answers with that no declaration describes.
//
//ergo:code NC-1067
//ergo:message The route {Endpoint} answers with {Member}, which no declaration describes.
type MemberUndeclared struct {
	Endpoint string
	Member   string
}

// APIRefused is a declared API with declarations it cannot hold, in source order.
//
//ergo:code NC-1062
//ergo:message The API's declarations were refused.
type APIRefused struct{ Refusals []Refusal }
