package assembling_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
)

// at is a position in a file every case declares its routes in.
func at(line int) assembling.Position {
	return assembling.Position{File: "routes.src", Line: line, Column: 1}
}

// found is the member every route succeeds with.
func found() description.Member {
	return description.Member{Name: "Found", Answer: description.Succeeds{Status: description.SuccessStatus{}}, Fields: []description.Field{}}
}

// route is a GET route that reads nothing and succeeds with Found.
func route(t *testing.T, name string, path string, line int) assembling.DeclaredEndpoint {
	t.Helper()
	return assembling.DeclaredEndpoint{
		Name: name, Method: description.Get{}, Path: routePath(t, path), Params: []description.Field{}, Query: []description.Field{}, Headers: []assembling.DeclaredHeader{},
		Body: description.NoBody{}, Successes: description.Successes{First: found(), Rest: []description.Member{}},
		Failures: []description.Member{}, Position: at(line),
	}
}

// routePath is text read as a route's path.
func routePath(t *testing.T, text string) description.RoutePath {
	t.Helper()
	switch read := description.ParseRoutePath(text).(type) {
	case description.RoutePathRead:
		return read.Path
	case description.RoutePathRefused:
		t.Fatalf("%q is not a route's path: %#v", text, read.Problem)
	case nil:
	}
	t.Fatalf("%q was read as no route's path", text)
	return description.RoutePath{}
}

// fieldName is text read as a field's name.
func fieldName(t *testing.T, text string) description.FieldName {
	t.Helper()
	switch read := description.ParseFieldName(text).(type) {
	case description.FieldNameRead:
		return read.Name
	case description.FieldNameRefused:
		t.Fatalf("%q is not a field's name: %#v", text, read.Problem)
	case nil:
	}
	t.Fatalf("%q was read as no field's name", text)
	return description.FieldName{}
}

// apiName is text read as the name of the API's class.
func apiName(t *testing.T, text string) description.APIName {
	t.Helper()
	switch read := description.ParseAPIName(text).(type) {
	case description.APINameRead:
		return read.Name
	case description.APINameNotIdentifier:
		t.Fatalf("%q cannot name the API's class: %#v", text, read.Flaw)
	case nil:
	}
	t.Fatalf("%q was read as no API name", text)
	return description.APIName{}
}

// api is an API of one group holding endpoints and the member Found.
func api(t *testing.T, endpoints ...assembling.DeclaredEndpoint) assembling.Declared {
	t.Helper()
	return assembling.Declared{
		API:     assembling.NamedAPI{Name: apiName(t, "ShopApi"), Position: at(0)},
		Groups:  []assembling.DeclaredGroup{{Name: "shop", Position: at(1), Endpoints: endpoints}},
		Members: []assembling.DeclaredMember{{Member: found(), Position: at(100)}},
		Structs: []assembling.DeclaredStruct{},
	}
}

func refusedCodes(t *testing.T, declared assembling.Declared) []string {
	t.Helper()
	switch assembled := assembling.Assemble(declared).(type) {
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
	text := description.Field{Name: fieldName(t, "item_id"), Schema: description.Text{}}
	withParams := func(endpoint assembling.DeclaredEndpoint, params ...description.Field) assembling.DeclaredEndpoint {
		endpoint.Params = params
		return endpoint
	}
	withBody := route(t, "readBody", "/body", 5)
	withBody.Body = description.JSONBody{Schema: description.Text{}}
	withRecordQuery := route(t, "list", "/items", 2)
	withRecordQuery.Query = []description.Field{{Name: fieldName(t, "filter"), Schema: description.StructRef{Name: "Filter"}}}
	repeatsShared := api(t, route(t, "list", "/items", 2))
	repeatsShared.Members = append(repeatsShared.Members, assembling.DeclaredMember{
		Member: description.Member{Name: "RequestRefused", Answer: description.Succeeds{Status: description.SuccessStatus{}}, Fields: []description.Field{}}, Position: at(101),
	})
	reservedField := api(t, route(t, "list", "/items", 2))
	reservedField.Members[0].Member.Fields = []description.Field{{Name: description.CodeField(), Schema: description.Text{}}}
	repeatsFailureCode := api(t, route(t, "list", "/items", 2))
	sharedFailure := description.SharedFailures()[0]
	repeatsFailureCode.Members = append(repeatsFailureCode.Members, assembling.DeclaredMember{
		Member: description.Member{Name: "DifferentFailure", Answer: sharedFailure.Member().Answer, Fields: []description.Field{}}, Position: at(102),
	})
	for _, each := range []struct {
		name     string
		declared assembling.Declared
		want     []string
	}{
		{name: "a path segment no field reads", declared: api(t, route(t, "read", "/items/{item_id}", 2)), want: []string{"NC-1022"}},
		{name: "a field reading no path segment", declared: api(t, withParams(route(t, "read", "/items", 2), text)), want: []string{"NC-1023"}},
		{
			name:     "a number type unsupported in a path segment",
			declared: api(t, withParams(route(t, "read", "/items/{item_id}", 2), description.Field{Name: text.Name, Schema: description.Finite{}})),
			want:     []string{"NC-1138"},
		},
		{name: "a record in one query parameter", declared: api(t, withRecordQuery), want: []string{"NC-1138"}},
		{name: "a body on a GET route", declared: api(t, withBody), want: []string{"NC-1025"}},
		{name: "two routes on one pattern", declared: api(t, route(t, "first", "/items", 2), route(t, "second", "/items", 3)), want: []string{"NC-1026"}},
		{
			name: "two routes on one path whose parameters are named apart",
			declared: api(t, withParams(route(t, "first", "/items/{item_id}", 2), text),
				withParams(route(t, "second", "/items/{id}", 3), description.Field{Name: fieldName(t, "id"), Schema: description.Text{}})),
			want: []string{"NC-1026"},
		},
		{name: "a member field named code", declared: reservedField, want: []string{"NC-1033"}},
		{name: "two routes of a group with one name", declared: api(t, route(t, "read", "/a", 2), route(t, "read", "/b", 3)), want: []string{"NC-1050"}},
		{name: "a member taking a shared failure's name", declared: repeatsShared, want: []string{"NC-1018"}},
		{name: "a distinct failure taking a shared failure's code", declared: repeatsFailureCode, want: []string{"NC-1139"}},
		{name: "a path parameter its field reads", declared: api(t, withParams(route(t, "read", "/items/{item_id}", 2), text)), want: []string{}},
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
	item := description.Field{Name: fieldName(t, "item_id"), Schema: description.Text{}}
	notFound := description.FoldStatus(404, func(status description.Status) description.FailureStatus {
		failure, isFailure := status.(description.FailureStatus)
		if !isFailure {
			t.Fatalf("404 parsed as %#v; want a failure status", status)
		}
		return failure
	}, func(cause description.StatusUnanswerable) description.FailureStatus {
		t.Fatalf("404: %v", cause)
		return description.FailureStatus{}
	})
	missing := description.Member{Name: "Missing", Answer: description.Fails{Status: notFound, Code: "IT-1001"}, Fields: []description.Field{item}}
	related := description.Member{Name: "Related", Answer: found().Answer, Fields: []description.Field{item}}
	reads := route(t, "read", "/items/{item_id}", 2)
	reads.Params = []description.Field{item}
	reads.Successes = description.Successes{First: related, Rest: []description.Member{found()}}
	reads.Failures = []description.Member{missing}
	declared := api(t, reads, route(t, "list", "/items", 3))
	declared.Members = append(declared.Members, assembling.DeclaredMember{Member: missing, Position: at(101)}, assembling.DeclaredMember{Member: related, Position: at(102)})
	shared := map[string]description.Member{}
	for _, failure := range description.SharedFailures() {
		shared[failure.Name] = failure.Member()
	}
	switch assembled := assembling.Assemble(declared).(type) {
	case assembling.APIAssembled:
		endpoints := assembled.API.Groups[0].Endpoints
		if len(endpoints) != 2 || endpoints[0].Name != "list" || endpoints[1].Name != "read" {
			t.Fatalf("described the endpoints %v; want list then read", endpoints)
		}
		wantPath := []description.PathSegment{
			description.PathText{Text: "items"},
			description.PathParameter{Field: description.Parameter{Name: item.Name, Schema: description.Text{}}},
		}
		if !slices.EqualFunc(endpoints[1].Path, wantPath, func(left, right description.PathSegment) bool { return left == right }) {
			t.Errorf("read's path is %v; want %v", endpoints[1].Path, wantPath)
		}
		for _, expected := range []struct {
			endpoint  description.Endpoint
			successes []description.Member
		}{
			{endpoint: endpoints[0], successes: []description.Member{found()}},
			{endpoint: endpoints[1], successes: []description.Member{found(), related}},
		} {
			if got, want := memberAnswers(expected.endpoint.Successes.Members()), memberAnswers(expected.successes); !slices.Equal(got, want) {
				t.Errorf("%s succeeds with %v; want %v", expected.endpoint.Name, got, want)
			}
		}
		readFailures := []description.Member{shared["AnswerNotWritten"], missing, shared["RequestRefused"]}
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
