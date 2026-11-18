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
	"github.com/CheckPickerUpper/nocontract/effect"
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
	Body      description.Body
	Successes []string
	Failures  []string
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

// Request is a declared API and every declaration its reader already refused.
type Request struct {
	Declared Declared
	Refused  []Refusal
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

// Assemble checks the declared API and describes it, refusing it when its reader refused any
// declaration or a check that spans routes refuses one.
func Assemble(request Request) Assembled {
	shared := description.SharedFailures()
	refusals := slices.Clone(request.Refused)
	// A name two members take is refused below; the first of them is the one a route answers with.
	members := map[string]description.Member{}
	for _, member := range slices.Backward(request.Declared.Members) {
		members[member.Member.Name] = member.Member
	}
	groups, endpoints := []description.Group{}, []DeclaredEndpoint{}
	for _, group := range request.Declared.Groups {
		described := []description.Endpoint{}
		for _, declared := range group.Endpoints {
			endpoints = append(endpoints, declared)
			switch checked := checkedEndpoint(declared, endpointMembers{declared: members, shared: shared}).(type) {
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
	refusals = append(refusals, repeatedNames(request.Declared, shared)...)
	refusals = append(refusals, reservedFields(request.Declared.Members)...)
	if len(refusals) > 0 {
		return APIRefused{Refusals: sortedRefusals(refusals)}
	}
	return APIAssembled{API: described(request.Declared, groups, shared)}
}

// described is the API's description: nocontract's own failures first, then every member and
// named struct as declared, and its groups.
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
	return description.API{Name: declared.API.Name, Groups: groups, Members: members, Structs: structs}
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

// endpointMembers is every member a route can answer with: the declared members, by name, and
// nocontract's own failures.
type endpointMembers struct {
	declared map[string]description.Member
	shared   []description.SharedFailure
}

// resolvedMembers is the members a route names, each found among the declared members, and a
// refusal for each name none of them takes.
type resolvedMembers struct {
	members  []description.Member
	refusals []Refusal
}

// resolved finds each member a route names among the declared members.
func (members endpointMembers) resolved(declared DeclaredEndpoint, names []string) resolvedMembers {
	found := resolvedMembers{members: []description.Member{}, refusals: []Refusal{}}
	for _, name := range names {
		outcome.FoldOption(keyed.Entry(members.declared, name), func(member description.Member) struct{} {
			found.members = append(found.members, member)
			return struct{}{}
		}, func() struct{} {
			found.refusals = append(found.refusals, Refusal{Position: declared.Position, Problem: MemberUndeclared{Endpoint: declared.Name, Member: name}})
			return struct{}{}
		})
	}
	return found
}

// checkedEndpoint binds a route's path, resolves the members it answers with, refuses a body its
// method's request does not carry and an outcome with no success, and adds every shared failure it
// can answer.
func checkedEndpoint(declared DeclaredEndpoint, members endpointMembers) endpointCheck {
	successes, failures := members.resolved(declared, declared.Successes), members.resolved(declared, declared.Failures)
	refusals := slices.Concat(bodyRefusals(declared), successes.refusals, failures.refusals)
	if len(declared.Successes) == 0 {
		refusals = append(refusals, Refusal{Position: declared.Position, Problem: EndpointWithoutSuccess{Endpoint: declared.Name}})
	}
	switch path := boundPath(declared).(type) {
	case pathBound:
		if len(refusals) > 0 {
			return endpointRefused{refusals: refusals}
		}
		return endpointChecked{endpoint: description.Endpoint{
			Name: declared.Name, Method: declared.Method, Path: path.segments, Query: declared.Query, Body: declared.Body,
			Successes: successes.members, Failures: append(failures.members, sharedFailureMembers(declared, members.shared)...),
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

// requestRead is an input reading a path segment, a query parameter or the body.
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
	if len(declared.Params) > 0 || len(declared.Query) > 0 {
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

// repeatedNames refuses every declaration taking a name the client already declares: the names its
// file imports and nocontract's own failures, which it declares itself, the API, each group's class,
// and every member and named struct.
func repeatedNames(declared Declared, shared []description.SharedFailure) []Refusal {
	claims := []nameClaim{}
	for _, name := range effect.ImportedNames() {
		claims = append(claims, nameClaim{name: name, takenBy: "a name the client imports", position: declared.API.Position})
	}
	for _, failure := range shared {
		claims = append(claims, nameClaim{name: failure.Name, takenBy: "one of nocontract's own failures", position: declared.API.Position})
	}
	claims = append(claims, nameClaim{name: declared.API.Name.Text(), takenBy: "the API's name", position: declared.API.Position})
	for _, group := range declared.Groups {
		claims = append(claims, nameClaim{name: description.GroupClassName(group.Name), takenBy: group.Position.String(), position: group.Position})
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
				refusals = append(refusals, Refusal{Position: member.Position, Problem: FieldNameReserved{Member: member.Member.Name, Field: field.Name}})
			}
		}
	}
	return refusals
}
