package openapi_test

import (
	"os"
	"path/filepath"
	"testing"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/openapi"
)

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

// answered is the answer a member with status number gives, failing with code when it is a
// failure status.
func answered(t *testing.T, number int, code ergo.Code) description.Answer {
	t.Helper()
	parsed := description.FoldStatus(number, func(status description.Status) description.Answer {
		switch typed := status.(type) {
		case description.SuccessStatus:
			return description.Succeeds{Status: typed}
		case description.FailureStatus:
			return description.Fails{Status: typed, Code: code}
		case nil:
		}
		return description.Succeeds{Status: description.SuccessStatus{}}
	}, func(cause description.StatusUnanswerable) description.Answer {
		t.Fatalf("status %d: %v", number, cause)
		return description.Succeeds{Status: description.SuccessStatus{}}
	})
	return parsed
}

// fieldMaker makes a field of a name read as a field's name and a schema.
func fieldMaker(t *testing.T) func(name string, schema description.Schema) description.Field {
	t.Helper()
	return func(name string, schema description.Schema) description.Field {
		t.Helper()
		switch read := description.ParseFieldName(name).(type) {
		case description.FieldNameRead:
			return description.Field{Name: read.Name, Schema: schema}
		case description.FieldNameRefused:
			t.Fatalf("%q is not a field's name: %#v", name, read.Problem)
		case nil:
		}
		t.Fatalf("%q was read as no field's name", name)
		return description.Field{Name: description.FieldName{}, Schema: schema}
	}
}

// itemAPI is one group of three routes, two sharing a path and one reading a body, two failures
// sharing a status, and a named struct holding every schema.
func itemAPI(t *testing.T) description.API {
	t.Helper()
	field := fieldMaker(t)
	identifier := description.Integer{Width: description.Unsigned64{}}
	itemID := field("item_id", identifier)
	itemParameter := description.PathParameter{Field: description.Parameter{Name: itemID.Name, Schema: identifier}}
	sortBy := field("sort", description.Text{})
	itemRead := description.Member{Name: "ItemRead", Answer: answered(t, 200, ""), Fields: []description.Field{field("item", description.StructRef{Name: "Item"})}}
	itemAdded := description.Member{Name: "ItemAdded", Answer: answered(t, 201, ""), Fields: []description.Field{field("id", description.Integer{Width: description.Signed64{}})}}
	itemRemoved := description.Member{Name: "ItemRemoved", Answer: answered(t, 200, ""), Fields: []description.Field{}}
	itemMissing := description.Member{Name: "ItemMissing", Answer: answered(t, 404, "IT-1001"), Fields: []description.Field{itemID}}
	shopClosed := description.Member{Name: "ShopClosed", Answer: answered(t, 404, "IT-1002"), Fields: []description.Field{field("reopens", description.Instant{})}}
	requestRefused := description.Member{Name: "RequestRefused", Answer: answered(t, 400, "NC-1001"), Fields: []description.Field{field("reason", description.Text{})}}
	return description.API{
		Name: apiName(t, "ItemApi"),
		Groups: []description.Group{{Name: "items", Endpoints: []description.Endpoint{
			{
				Name: "readItem", Method: description.Get{},
				Path:  []description.PathSegment{description.PathText{Text: "items"}, itemParameter},
				Query: []description.Parameter{{Name: sortBy.Name, Schema: description.Literals{Values: []string{"name", "price"}}}},
				Body:  description.NoBody{}, Successes: description.Successes{First: itemRead, Rest: []description.Member{}}, Failures: []description.Member{itemMissing, shopClosed, requestRefused},
			},
			{
				Name: "addItem", Method: description.Post{},
				Path:  []description.PathSegment{description.PathText{Text: "items"}},
				Query: []description.Parameter{},
				Body: description.JSONBody{Schema: description.InlineStruct{Fields: []description.Field{
					field("item", description.StructRef{Name: "Item"}),
				}}},
				Successes: description.Successes{First: itemAdded, Rest: []description.Member{}}, Failures: []description.Member{requestRefused},
			},
			{
				Name: "removeItem", Method: description.Delete{},
				Path:  []description.PathSegment{description.PathText{Text: "items"}, itemParameter},
				Query: []description.Parameter{}, Body: description.NoBody{},
				Successes: description.Successes{First: itemRemoved, Rest: []description.Member{}}, Failures: []description.Member{itemMissing},
			},
		}}},
		Members: []description.Member{itemRead, itemAdded, itemRemoved, itemMissing, shopClosed, requestRefused},
		Structs: []description.NamedStruct{
			{Name: "Item", Fields: []description.Field{
				field("name", description.Text{}),
				field("count", description.Integer{Width: description.Signed32{}}),
				field("weight", description.Finite{}),
				field("sold", description.Truth{}),
				field("added", description.Instant{}),
				field("size", description.Literals{Values: []string{"small", "large"}}),
				field("labels", description.List{Item: description.Text{}}),
				field("corners", description.FixedList{Item: description.Finite{}, Length: 4}),
				field("prices", description.Dictionary{Key: description.Text{}, Value: description.Finite{}}),
				field("stock", description.Dictionary{Key: description.Literals{Values: []string{"north", "south"}}, Value: description.Integer{Width: description.Unsigned32{}}}),
				field("discount", description.Nullable{Inner: description.StructRef{Name: "Price"}}),
				field("box", description.InlineStruct{Fields: []description.Field{field("width", description.Integer{Width: description.Unsigned16{}})}}),
			}},
			{Name: "Price", Fields: []description.Field{field("cents", description.Integer{Width: description.Signed8{}})}},
		},
	}
}

// The OpenAPI document of an API is exactly the committed one: every schema as its one JSON
// Schema, each route's parameters, body and members under their statuses, in a fixed key order.
func TestAnAPIIsWrittenAsItsOneOpenAPIDocument(t *testing.T) {
	t.Parallel()
	want := outcome.Fold(outcome.TupleToResult(os.ReadFile(filepath.Join("testdata", "item_api.openapi.json"))),
		func(content []byte) string { return string(content) },
		func(cause error) string {
			t.Fatalf("reading the expected document: %v", cause)
			return ""
		})
	got := outcome.Fold(openapi.Render(itemAPI(t), "2.1.0"), func(written []byte) string { return string(written) }, func(cause error) string {
		t.Fatalf("Render refused: %v", cause)
		return ""
	})
	if got != want {
		t.Fatalf("Render wrote:\n%s\nwant:\n%s", got, want)
	}
}
