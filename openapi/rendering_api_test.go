package openapi_test

import (
	"os"
	"path/filepath"
	"testing"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/openapi"
)

// answered is the answer a member with status number gives, failing with code when it is a
// failure status.
func answered(t *testing.T, number int, code ergo.Code) description.Answer {
	t.Helper()
	parsed := mo.Fold(description.ParseStatus(number), func(status description.Status) description.Answer {
		switch typed := status.(type) {
		case description.SuccessStatus:
			return description.Succeeds{Status: typed}
		case description.FailureStatus:
			return description.Fails{Status: typed, Code: code}
		case nil:
		}
		return description.Succeeds{Status: description.SuccessStatus{}}
	}, func(cause error) description.Answer {
		t.Fatalf("status %d: %v", number, cause)
		return description.Succeeds{Status: description.SuccessStatus{}}
	})
	return parsed
}

func field(name string, schema description.Schema) description.Field {
	return description.Field{Name: name, Schema: schema}
}

// itemAPI is one group of three routes, two sharing a path and one reading a body, two failures
// sharing a status, and a named struct holding every schema.
func itemAPI(t *testing.T) description.API {
	t.Helper()
	itemID := field("item_id", description.Integer{})
	return description.API{
		Name: "ItemApi",
		Groups: []description.Group{{Name: "items", Endpoints: []description.Endpoint{
			{
				Name: "readItem", Method: description.Get{},
				Path:  []description.PathSegment{description.PathText{Text: "items"}, description.PathParameter{Field: itemID}},
				Query: []description.Field{field("sort", description.Literals{Values: []string{"name", "price"}})},
				Body:  description.NoBody{}, Successes: []string{"ItemRead"}, Failures: []string{"ItemMissing", "ShopClosed", "RequestRefused"},
			},
			{
				Name: "addItem", Method: description.Post{},
				Path:  []description.PathSegment{description.PathText{Text: "items"}},
				Query: []description.Field{},
				Body: description.JSONBody{Schema: description.InlineStruct{Fields: []description.Field{
					field("item", description.StructRef{Name: "Item"}),
				}}},
				Successes: []string{"ItemAdded"}, Failures: []string{"RequestRefused"},
			},
			{
				Name: "removeItem", Method: description.Delete{},
				Path:  []description.PathSegment{description.PathText{Text: "items"}, description.PathParameter{Field: itemID}},
				Query: []description.Field{}, Body: description.NoBody{},
				Successes: []string{"ItemRemoved"}, Failures: []string{"ItemMissing"},
			},
		}}},
		Members: []description.Member{
			{Name: "ItemRead", Answer: answered(t, 200, ""), Fields: []description.Field{field("item", description.StructRef{Name: "Item"})}},
			{Name: "ItemAdded", Answer: answered(t, 201, ""), Fields: []description.Field{field("id", description.Integer{})}},
			{Name: "ItemRemoved", Answer: answered(t, 200, ""), Fields: []description.Field{}},
			{Name: "ItemMissing", Answer: answered(t, 404, "IT-1001"), Fields: []description.Field{itemID}},
			{Name: "ShopClosed", Answer: answered(t, 404, "IT-1002"), Fields: []description.Field{field("reopens", description.Instant{})}},
			{Name: "RequestRefused", Answer: answered(t, 400, "NC-1001"), Fields: []description.Field{field("reason", description.Text{})}},
		},
		Structs: []description.NamedStruct{
			{Name: "Item", Fields: []description.Field{
				field("name", description.Text{}),
				field("count", description.Integer{}),
				field("weight", description.Finite{}),
				field("sold", description.Truth{}),
				field("added", description.Instant{}),
				field("size", description.Literals{Values: []string{"small", "large"}}),
				field("labels", description.List{Item: description.Text{}}),
				field("prices", description.Dictionary{Value: description.Finite{}}),
				field("discount", description.Nullable{Inner: description.StructRef{Name: "Price"}}),
				field("box", description.InlineStruct{Fields: []description.Field{field("width", description.Integer{})}}),
			}},
			{Name: "Price", Fields: []description.Field{field("cents", description.Integer{})}},
		},
	}
}

// The OpenAPI document of an API is exactly the committed one: every schema as its one JSON
// Schema, each route's parameters, body and members under their statuses, in a fixed key order.
func TestAnAPIIsWrittenAsItsOneOpenAPIDocument(t *testing.T) {
	t.Parallel()
	want := mo.Fold(mo.TupleToResult(os.ReadFile(filepath.Join("testdata", "item_api.openapi.json"))),
		func(content []byte) string { return string(content) },
		func(cause error) string {
			t.Fatalf("reading the expected document: %v", cause)
			return ""
		})
	got := mo.Fold(openapi.Render(itemAPI(t), "2.1.0"), func(written []byte) string { return string(written) }, func(cause error) string {
		t.Fatalf("Render refused: %v", cause)
		return ""
	})
	if got != want {
		t.Fatalf("Render wrote:\n%s\nwant:\n%s", got, want)
	}
}
