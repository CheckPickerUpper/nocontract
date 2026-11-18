package assembling_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
)

// at is a position in a file every case declares its routes in.
func at(line int) assembling.Position {
	return assembling.Position{File: "routes.src", Line: line, Column: 1}
}

// route is a GET route that reads nothing and succeeds with Found.
func route(name string, path string, line int) assembling.DeclaredEndpoint {
	return assembling.DeclaredEndpoint{
		Name: name, Method: description.Get{}, Path: path, Params: []description.Field{}, Query: []description.Field{},
		Body: description.NoBody{}, Successes: []string{"Found"}, Failures: []string{}, Position: at(line),
	}
}

// api is an API of one group holding endpoints and the member Found.
func api(endpoints ...assembling.DeclaredEndpoint) assembling.Declared {
	return assembling.Declared{
		API:    assembling.NamedAPI{Name: "ShopApi", Position: at(0)},
		Groups: []assembling.DeclaredGroup{{Name: "shop", Position: at(1), Endpoints: endpoints}},
		Members: []assembling.DeclaredMember{{
			Member: description.Member{Name: "Found", Answer: description.Succeeds{Status: description.SuccessStatus{}}, Fields: []description.Field{}}, Position: at(100),
		}},
		Structs: []assembling.DeclaredStruct{},
	}
}

func refusedCodes(t *testing.T, declared assembling.Declared) []string {
	t.Helper()
	switch assembled := assembling.Assemble(assembling.Request{Declared: declared, Refused: []assembling.Refusal{}}).(type) {
	case assembling.APIRefused:
		codes := []string{}
		for _, refusal := range assembled.Refusals {
			codes = append(codes, refusal.Problem.ErrorCode().String())
		}
		slices.Sort(codes)
		return codes
	case assembling.APIAssembled:
		return []string{}
	case nil:
	}
	t.Fatal("Assemble answered nothing")
	return []string{}
}

func TestEachRuleSpanningRoutesRefusesWithItsCode(t *testing.T) {
	t.Parallel()
	text := description.Field{Name: "item_id", Schema: description.Text{}}
	withParams := func(endpoint assembling.DeclaredEndpoint, params ...description.Field) assembling.DeclaredEndpoint {
		endpoint.Params = params
		return endpoint
	}
	withBody := route("readBody", "/body", 5)
	withBody.Body = description.JSONBody{Schema: description.Text{}}
	succeedsNever := route("never", "/never", 6)
	succeedsNever.Successes = []string{}
	repeatsShared := api(route("list", "/items", 2))
	repeatsShared.Members = append(repeatsShared.Members, assembling.DeclaredMember{
		Member: description.Member{Name: "RequestRefused", Answer: description.Succeeds{Status: description.SuccessStatus{}}, Fields: []description.Field{}}, Position: at(101),
	})
	reservedField := api(route("list", "/items", 2))
	reservedField.Members[0].Member.Fields = []description.Field{{Name: "code", Schema: description.Text{}}}
	answersUndeclared := route("read", "/items", 2)
	answersUndeclared.Failures = []string{"Missing"}
	for _, each := range []struct {
		name     string
		declared assembling.Declared
		want     []string
	}{
		{name: "a path segment no field reads", declared: api(route("read", "/items/{item_id}", 2)), want: []string{"NC-1022"}},
		{name: "a field reading no path segment", declared: api(withParams(route("read", "/items", 2), text)), want: []string{"NC-1023"}},
		{name: "a wildcard segment", declared: api(route("read", "/files/{rest...}", 2)), want: []string{"NC-1024"}},
		{name: "a brace inside a segment", declared: api(route("read", "/files/a{b}", 2)), want: []string{"NC-1024"}},
		{name: "a body on a GET route", declared: api(withBody), want: []string{"NC-1025"}},
		{name: "two routes on one pattern", declared: api(route("first", "/items", 2), route("second", "/items", 3)), want: []string{"NC-1026"}},
		{
			name: "two routes on one path whose parameters are named apart",
			declared: api(withParams(route("first", "/items/{item_id}", 2), text),
				withParams(route("second", "/items/{id}", 3), description.Field{Name: "id", Schema: description.Text{}})),
			want: []string{"NC-1026"},
		},
		{name: "a route with no success", declared: api(succeedsNever), want: []string{"NC-1028"}},
		{name: "a member field named code", declared: reservedField, want: []string{"NC-1033"}},
		{name: "two routes of a group with one name", declared: api(route("read", "/a", 2), route("read", "/b", 3)), want: []string{"NC-1050"}},
		{name: "a member taking a shared failure's name", declared: repeatsShared, want: []string{"NC-1018"}},
		{name: "a route answering a member nothing declares", declared: api(answersUndeclared), want: []string{"NC-1067"}},
		{name: "a path parameter its field reads", declared: api(withParams(route("read", "/items/{item_id}", 2), text)), want: []string{}},
	} {
		if got := refusedCodes(t, each.declared); !slices.Equal(got, each.want) {
			t.Errorf("%s was refused with %v; want %v", each.name, got, each.want)
		}
	}
}

// memberAnswers is each member's name and how it answers, which a route's members are compared by.
func memberAnswers(members []description.Member) []string {
	answers := []string{}
	for _, member := range members {
		answers = append(answers, fmt.Sprintf("%s %#v", member.Name, member.Answer))
	}
	return answers
}

func TestARouteIsDescribedWithItsBoundPathAndTheMembersItAnswersWith(t *testing.T) {
	t.Parallel()
	item := description.Field{Name: "item_id", Schema: description.Text{}}
	reads := route("read", "/items/{item_id}", 2)
	reads.Params = []description.Field{item}
	reads.Failures = []string{"Missing"}
	declared := api(reads, route("list", "/items", 3))
	notFound := outcome.Fold(description.ParseStatus(404), func(status description.Status) description.FailureStatus {
		failure, isFailure := status.(description.FailureStatus)
		if !isFailure {
			t.Fatalf("404 parsed as %#v; want a failure status", status)
		}
		return failure
	}, func(cause error) description.FailureStatus {
		t.Fatalf("404: %v", cause)
		return description.FailureStatus{}
	})
	missing := description.Member{Name: "Missing", Answer: description.Fails{Status: notFound, Code: "IT-1001"}, Fields: []description.Field{item}}
	declared.Members = append(declared.Members, assembling.DeclaredMember{Member: missing, Position: at(101)})
	found := declared.Members[0].Member
	shared := map[string]description.Member{}
	for _, failure := range description.SharedFailures() {
		shared[failure.Name] = failure.Member()
	}
	switch assembled := assembling.Assemble(assembling.Request{Declared: declared, Refused: []assembling.Refusal{}}).(type) {
	case assembling.APIAssembled:
		endpoints := assembled.API.Groups[0].Endpoints
		if len(endpoints) != 2 || endpoints[0].Name != "list" || endpoints[1].Name != "read" {
			t.Fatalf("described the endpoints %v; want list then read", endpoints)
		}
		wantPath := []description.PathSegment{description.PathText{Text: "items"}, description.PathParameter{Field: item}}
		if !slices.EqualFunc(endpoints[1].Path, wantPath, func(left, right description.PathSegment) bool { return left == right }) {
			t.Errorf("read's path is %v; want %v", endpoints[1].Path, wantPath)
		}
		for _, endpoint := range endpoints {
			if got, want := memberAnswers(endpoint.Successes), memberAnswers([]description.Member{found}); !slices.Equal(got, want) {
				t.Errorf("%s succeeds with %v; want %v", endpoint.Name, got, want)
			}
		}
		readFailures := []description.Member{missing, shared["RequestRefused"], shared["AnswerNotWritten"]}
		if got, want := memberAnswers(endpoints[1].Failures), memberAnswers(readFailures); !slices.Equal(got, want) {
			t.Errorf("read, which reads its path, fails with %v; want %v", got, want)
		}
		if got, want := memberAnswers(endpoints[0].Failures), memberAnswers([]description.Member{shared["AnswerNotWritten"]}); !slices.Equal(got, want) {
			t.Errorf("list, which reads nothing, fails with %v; want %v", got, want)
		}
	case assembling.APIRefused:
		t.Fatalf("refused %v; want the API described", assembled.Refusals)
	case nil:
		t.Fatal("Assemble answered nothing")
	}
}
