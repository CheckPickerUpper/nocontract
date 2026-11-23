// Package assembling turns the API one backend's reader declared into the description every writer
// reads. Whatever language the backend is written in, it runs once the checks that span routes and
// the rules every route shares: each path bound to the fields that read it, nocontract's own
// failures added to each route that can answer them, and every name the client declares taken
// once.
package assembling

import (
	"cmp"
	"slices"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

// Declared is an API as one backend's reader declared it: its name, each group of routes, and
// every member and named struct those routes use, each declaration once, in the order read.
type Declared struct {
	API     NamedAPI
	Groups  []DeclaredGroup
	Members []DeclaredMember
	Structs []DeclaredStruct
}

// NamedAPI is the API's name and where it is given.
type NamedAPI struct {
	Name     description.APIName
	Position Position
}

// DeclaredGroup is the routes one module of the backend declares, named for the module, and where
// its first route is written.
type DeclaredGroup struct {
	Name      string
	Position  Position
	Endpoints []DeclaredEndpoint
}

// DeclaredEndpoint is one route as its reader read it: its name, method and path, the fields
// that read its path's parameters, by name, and its query and body, the members its outcome
// succeeds with, every member it fails with outside nocontract's own failures, and where it is
// written.
type DeclaredEndpoint struct {
	Name      string
	Method    description.Method
	Path      description.RoutePath
	Params    []description.Field
	Query     []description.Field
	Headers   []DeclaredHeader
	Body      description.Body
	Successes description.Successes
	Failures  []description.Member
	Position  Position
}

// DeclaredMember is one member of an outcome or a guard's refusal, and where it is declared.
type DeclaredMember struct {
	Member   description.Member
	Position Position
}

// DeclaredStruct is one named struct a member or a body uses, and where it is declared.
type DeclaredStruct struct {
	Struct   description.NamedStruct
	Position Position
}

// Assembled is what assembling an API ended as.
//
//sumtype:decl
type Assembled interface{ isAssembled() }

// APIAssembled is the description of an API nothing refused.
//
//ergo:not-an-error
type APIAssembled struct{ API description.API }

func (APIAssembled) isAssembled() {}
func (APIRefused) isAssembled()   {}

// Assemble checks an API its reader read whole and describes it, refusing it when a check that
// spans routes refuses a declaration.
func Assemble(declared Declared) Assembled {
	checked := checkedAPI(declared)
	if len(checked.refusals) > 0 {
		return APIRefused{Refusals: sortedRefusals(checked.refusals)}
	}
	return APIAssembled{API: described(declared, checked.groups, checked.shared)}
}

// Refuse is an API its reader refused declarations of, with the reader's refusals and every one the
// checks that span routes find among the declarations it did read.
func Refuse(declared Declared, refused []Refusal) APIRefused {
	return APIRefused{Refusals: sortedRefusals(slices.Concat(refused, checkedAPI(declared).refusals))}
}

// apiCheck is a declared API's groups as described, nocontract's own failures, and every refusal
// the checks that span routes find.
type apiCheck struct {
	groups   []description.Group
	shared   []description.SharedFailure
	refusals []Refusal
}

// checkedAPI describes each group of routes and runs every check that spans routes.
func checkedAPI(declared Declared) apiCheck {
	shared := description.SharedFailures()
	refusals := []Refusal{}
	groups, endpoints := []description.Group{}, []DeclaredEndpoint{}
	for _, group := range declared.Groups {
		described := []description.Endpoint{}
		for _, endpoint := range group.Endpoints {
			endpoints = append(endpoints, endpoint)
			switch checked := checkedEndpoint(endpoint, shared).(type) {
			case endpointChecked:
				described = append(described, checked.endpoint)
			case endpointRefused:
				refusals = append(refusals, checked.refusals...)
			case nil:
			}
		}
		slices.SortFunc(described, func(left, right description.Endpoint) int { return cmp.Compare(left.Name, right.Name) })
		groups = append(groups, description.Group{Name: group.Name, Endpoints: described})
		refusals = append(refusals, repeatedEndpointNames(group)...)
	}
	slices.SortFunc(groups, func(left, right description.Group) int { return cmp.Compare(left.Name, right.Name) })
	refusals = append(refusals, repeatedRoutes(endpoints)...)
	refusals = append(refusals, repeatedNames(declared, shared)...)
	refusals = append(refusals, repeatedFailureCodes(declared.Members, shared)...)
	refusals = append(refusals, reservedFields(declared.Members)...)
	return apiCheck{groups: groups, shared: shared, refusals: refusals}
}

// described is the API's description, with members and named structs ordered by public name.
func described(declared Declared, groups []description.Group, shared []description.SharedFailure) description.API {
	members := []description.Member{}
	for _, failure := range shared {
		members = append(members, failure.Member())
	}
	for _, member := range declared.Members {
		members = append(members, member.Member)
	}
	structs := []description.NamedStruct{}
	for _, named := range declared.Structs {
		structs = append(structs, named.Struct)
	}
	slices.SortStableFunc(structs, func(left, right description.NamedStruct) int { return cmp.Compare(left.Name, right.Name) })
	sortMembersByName(members)
	return description.API{Name: declared.API.Name, Groups: groups, Members: members, Structs: structs}
}

func sortMembersByName(members []description.Member) {
	slices.SortStableFunc(members, func(left, right description.Member) int { return cmp.Compare(left.Name, right.Name) })
}

// endpointCheck is one route checked on its own: described, or every reason it cannot be.
//
//sumtype:decl
type endpointCheck interface{ isEndpointCheck() }

// endpointChecked is a route described whole.
//
//ergo:not-an-error
type endpointChecked struct{ endpoint description.Endpoint }

// endpointRefused is a route refused, with every reason.
//
//ergo:not-an-error
type endpointRefused struct{ refusals []Refusal }

func (endpointChecked) isEndpointCheck() {}
func (endpointRefused) isEndpointCheck() {}

// checkedEndpoint binds a route's path, refuses a body its method's request does not carry, and adds
// every shared failure it can answer.
func checkedEndpoint(declared DeclaredEndpoint, shared []description.SharedFailure) endpointCheck {
	refusals := bodyRefusals(declared)
	headers := checkedHeaders(declared.Headers)
	refusals = append(refusals, headers.refusals...)
	query := make([]description.Parameter, 0, len(declared.Query))
	for _, field := range declared.Query {
		outcome.FoldOption(description.ParseParameter(field), func(parameter description.Parameter) struct{} {
			query = append(query, parameter)
			return struct{}{}
		}, func() struct{} {
			refusals = append(refusals, Refusal{Position: declared.Position, Problem: parameterNotReadable(field, "query")})
			return struct{}{}
		})
	}
	switch path := boundPath(declared).(type) {
	case pathBound:
		if len(refusals) > 0 {
			return endpointRefused{refusals: refusals}
		}
		successes := declared.Successes.Members()
		failures := slices.Concat(declared.Failures, sharedFailureMembers(declared, shared))
		sortMembersByName(successes)
		sortMembersByName(failures)
		return endpointChecked{endpoint: description.Endpoint{
			Name: declared.Name, Method: declared.Method, Path: path.segments, Query: query, Headers: headers.parameters, Body: declared.Body,
			Successes: description.Successes{First: successes[0], Rest: successes[1:]},
			Failures:  failures,
		}}
	case pathRefused:
		return endpointRefused{refusals: append(refusals, path.refusals...)}
	case nil:
	}
	return endpointRefused{refusals: refusals}
}

// bodyRefusals refuses a route reading a body its method's request does not carry.
func bodyRefusals(declared DeclaredEndpoint) []Refusal {
	switch declared.Method.Body().(type) {
	case description.BodyAbsent:
		switch declared.Body.(type) {
		case description.JSONBody:
			return []Refusal{{Position: declared.Position, Problem: BodyWithoutRequestBody{Endpoint: declared.Name, Method: declared.Method.Name()}}}
		case description.NoBody, nil:
		}
	case description.BodyCarried, nil:
	}
	return []Refusal{}
}

// requestUse is whether a route's input reads any part of its request, which it can then refuse.
//
//sumtype:decl
type requestUse interface{ isRequestUse() }

// requestRead is an input reading a path segment, a query parameter, a header or the body.
//
//ergo:not-an-error
type requestRead struct{}

// requestUnread is an input reading nothing from the request.
//
//ergo:not-an-error
type requestUnread struct{}

func (requestRead) isRequestUse()   {}
func (requestUnread) isRequestUse() {}

// requestUseDeclared is whether the route's input reads any part of its request.
func requestUseDeclared(declared DeclaredEndpoint) requestUse {
	switch declared.Body.(type) {
	case description.JSONBody:
		return requestRead{}
	case description.NoBody, nil:
	}
	if len(declared.Params) > 0 || len(declared.Query) > 0 || len(declared.Headers) > 0 {
		return requestRead{}
	}
	return requestUnread{}
}

// sharedFailureMembers is every shared failure the route can answer, in the order declared.
func sharedFailureMembers(declared DeclaredEndpoint, shared []description.SharedFailure) []description.Member {
	members := []description.Member{}
	for _, failure := range shared {
		switch failure.Answers.(type) {
		case description.AnsweredByEveryRoute:
			members = append(members, failure.Member())
		case description.AnsweredWhenRequestRead:
			switch requestUseDeclared(declared).(type) {
			case requestRead:
				members = append(members, failure.Member())
			case requestUnread, nil:
			}
		case description.AnsweredWithoutRoute:
		case nil:
		}
	}
	return members
}

// repeatedRoutes refuses every route serving a method and path an earlier route serves. Two paths
// differing only in their parameters' names match the same requests, so they are one path.
func repeatedRoutes(endpoints []DeclaredEndpoint) []Refusal {
	pattern := func(declared DeclaredEndpoint) string { return declared.Method.Name() + " " + declared.Path.Text() }
	served := func(declared DeclaredEndpoint) string { return declared.Method.Name() + " " + declared.Path.Matched() }
	refusals := []Refusal{}
	for _, repeat := range keyed.Repeats(endpoints, served) {
		refusals = append(refusals, Refusal{Position: repeat.Item.Position, Problem: RouteRepeated{Pattern: pattern(repeat.Item), First: repeat.First.Position.String()}})
	}
	return refusals
}

// repeatedEndpointNames refuses every route of a group whose name an earlier route of the group
// takes.
func repeatedEndpointNames(group DeclaredGroup) []Refusal {
	refusals := []Refusal{}
	for _, repeat := range keyed.Repeats(group.Endpoints, func(declared DeclaredEndpoint) string { return declared.Name }) {
		refusals = append(refusals, Refusal{Position: repeat.Item.Position, Problem: EndpointNameRepeated{Name: repeat.Item.Name, First: repeat.First.Position.String()}})
	}
	return refusals
}

// nameClaim is one name the client declares, what declares it, and where.
type nameClaim struct {
	name     string
	takenBy  string
	position Position
}

// repeatedNames prevents the API and its schemas from claiming the same public name.
func repeatedNames(declared Declared, shared []description.SharedFailure) []Refusal {
	claims := []nameClaim{{name: declared.API.Name.Text(), takenBy: "the API's name", position: declared.API.Position}}
	for _, failure := range shared {
		claims = append(claims, nameClaim{name: failure.Name, takenBy: "one of nocontract's own failures", position: declared.API.Position})
	}
	for _, member := range declared.Members {
		claims = append(claims, nameClaim{name: member.Member.Name, takenBy: member.Position.String(), position: member.Position})
	}
	for _, named := range declared.Structs {
		claims = append(claims, nameClaim{name: named.Struct.Name, takenBy: named.Position.String(), position: named.Position})
	}
	refusals := []Refusal{}
	for _, repeat := range keyed.Repeats(claims, func(claim nameClaim) string { return claim.name }) {
		refusals = append(refusals, Refusal{Position: repeat.Item.position, Problem: NameRepeated{Name: repeat.Item.name, First: repeat.First.takenBy}})
	}
	return refusals
}

// reservedFields refuses every member field whose JSON name is one every answer starts with.
func reservedFields(members []DeclaredMember) []Refusal {
	refusals := []Refusal{}
	for _, member := range members {
		for _, field := range member.Member.Fields {
			if slices.Contains(description.ReservedFields(), field.Name) {
				refusals = append(refusals, Refusal{Position: member.Position, Problem: FieldNameReserved{Member: member.Member.Name, Field: field.Name.Text()}})
			}
		}
	}
	return refusals
}
