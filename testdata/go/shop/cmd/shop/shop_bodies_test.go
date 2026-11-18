package main

import (
	"bytes"
	"encoding/json/jsontext"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/santhosh-tekuri/jsonschema/v6"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

// publishedDocument is the OpenAPI document the shop publishes for every other client.
const publishedDocument = "../../shop_api.openapi.json"

// notCanonical ends the reason a string a 64-bit field holds is refused for when it is not canonical
// decimal text.
const notCanonical = "which is not a whole number written as canonical decimal text: an optional -, then 0 or a digit from 1 to 9 followed by any digits, " +
	"and never -0."

// bodyRefused is the answer refusing a body at name, which reason explains, as JSON writes it.
func bodyRefused(name, reason string) received {
	return received{status: 400, allow: "", header: "", body: `{"_tag":"RequestRefused","code":"NC-1001","part":"body","name":"` + name + `","reason":"` + reason + `"}`}
}

// requestSchema is the JSON Schema the published document declares for the body a POST to route
// reads, route written as the document writes its paths, compiled as JSON Schema 2020-12 with
// every format asserted.
func requestSchema(t *testing.T, route string) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	document := mo.Fold(mo.TupleToResult(os.ReadFile(publishedDocument)), func(content []byte) any {
		return mo.Fold(mo.TupleToResult(jsonschema.UnmarshalJSON(bytes.NewReader(content))), func(read any) any { return read }, func(cause error) any {
			t.Fatalf("reading %s: %v", publishedDocument, cause)
			return nil
		})
	}, func(cause error) any {
		t.Fatalf("reading %s: %v", publishedDocument, cause)
		return nil
	})
	mo.EmptyableToOption(compiler.AddResource(publishedDocument, document)).ForEach(func(cause error) {
		t.Fatalf("adding %s: %v", publishedDocument, cause)
	})
	pointer := []string{}
	for _, token := range []string{"paths", route, "post", "requestBody", "content", "application/json", "schema"} {
		pointer = append(pointer, url.PathEscape(strings.NewReplacer("~", "~0", "/", "~1").Replace(token)))
	}
	location := publishedDocument + "#/" + strings.Join(pointer, "/")
	return mo.Fold(mo.TupleToResult(compiler.Compile(location)), func(compiled *jsonschema.Schema) *jsonschema.Schema { return compiled },
		func(cause error) *jsonschema.Schema {
			t.Fatalf("compiling %s: %v", location, cause)
			return nil
		})
}

// Every body a route reads is taken or refused by the server exactly as the request schema the
// shop publishes takes or refuses it, judged by a JSON Schema 2020-12 validator, whenever the body
// is one JSON value whose objects each name every member once, which is all a schema can judge.
func TestARouteReadsItsBodyWholeOrRefusesItWhereItIsWrong(t *testing.T) {
	served := shopServer(t, []nocontract.Layer{})
	const topUps, orders = "/shoppers/7/top-ups", "/shoppers/7/orders"
	topUpSchema, orderSchema := requestSchema(t, "/shoppers/{shopper_id}/top-ups"), requestSchema(t, "/shoppers/{shopper_id}/orders")
	order := func(items, deliverBetween, wrapped, courierNotes string) string {
		return `{"items":` + items + `,"gift_note":null,"deliver_between":` + deliverBetween + `,"wrapped":` + wrapped + `,"tips":{"CAD":2.5},"courier_notes":` +
			courierNotes + `}`
	}
	const tea, december, porch = `[{"sku":"tea","quantity":2}]`, `["2026-12-01T10:00:00+01:00","2026-12-02T09:00:00Z"]`, `{"drone":"the porch"}`
	cases := []struct {
		name   string
		path   string
		schema *jsonschema.Schema
		body   string
		want   received
	}{
		{
			name: "a whole top-up", path: topUps, schema: topUpSchema, body: `{"amount":{"cents":"500","currency":"CAD"}}`,
			want: received{status: 202, allow: "", header: "", body: `{"_tag":"TopUpQueued","reference":"CAD"}`},
		},
		{
			name: "the most a 64-bit amount holds, as decimal text", path: topUps, schema: topUpSchema,
			body: `{"amount":{"cents":"9223372036854775807","currency":"CAD"}}`,
			want: received{status: 202, allow: "", header: "", body: `{"_tag":"TopUpQueued","reference":"CAD"}`},
		},
		{
			name: "a member left out", path: topUps, schema: topUpSchema, body: `{"amount":{"cents":"500"}}`,
			want: bodyRefused("body.amount.currency", "body.amount.currency is missing from the body."),
		},
		{
			name: "a member the body does not declare", path: topUps, schema: topUpSchema, body: `{"amount":{"cents":"500","currency":"CAD"},"tip":5}`,
			want: bodyRefused("body.tip", "body.tip is not a member the body accepts."),
		},
		{
			name: "a value outside its set", path: topUps, schema: topUpSchema, body: `{"amount":{"cents":"500","currency":"EUR"}}`,
			want: bodyRefused("body.amount.currency", `body.amount.currency holds \"EUR\", which is not one of the values it accepts.`),
		},
		{
			name: "decimal text outside its field's width", path: topUps, schema: topUpSchema, body: `{"amount":{"cents":"9223372036854775808","currency":"CAD"}}`,
			want: bodyRefused("body.amount.cents", `body.amount.cents holds \"9223372036854775808\", which is outside -9223372036854775808 to 9223372036854775807.`),
		},
		{
			name: "decimal text with a leading zero", path: topUps, schema: topUpSchema, body: `{"amount":{"cents":"007","currency":"CAD"}}`,
			want: bodyRefused("body.amount.cents", `body.amount.cents holds \"007\", `+notCanonical),
		},
		{
			name: "decimal text with a plus sign", path: topUps, schema: topUpSchema, body: `{"amount":{"cents":"+7","currency":"CAD"}}`,
			want: bodyRefused("body.amount.cents", `body.amount.cents holds \"+7\", `+notCanonical),
		},
		{
			name: "decimal text of minus zero", path: topUps, schema: topUpSchema, body: `{"amount":{"cents":"-0","currency":"CAD"}}`,
			want: bodyRefused("body.amount.cents", `body.amount.cents holds \"-0\", `+notCanonical),
		},
		{
			name: "a JSON number where a 64-bit amount is decimal text", path: topUps, schema: topUpSchema, body: `{"amount":{"cents":500,"currency":"CAD"}}`,
			want: bodyRefused("body.amount.cents", "body.amount.cents must hold a whole number written as a string, and it holds 500."),
		},
		{
			name: "an empty body", path: topUps, schema: topUpSchema, body: "",
			want: bodyRefused("body", "The body holds no JSON value."),
		},
		{
			name: "a whole order", path: orders, schema: orderSchema, body: order(tea, december, "true", porch),
			want: received{status: 201, allow: "", header: "", body: `{"_tag":"OrderPlaced","order":{"items":[{"sku":"tea","quantity":2}],"gift_note":null,` +
				`"deliver_between":["2026-12-01T09:00:00Z","2026-12-02T09:00:00Z"],"wrapped":true,"tips":{"CAD":2.5},"courier_notes":{"drone":"the porch"}}}`},
		},
		{
			name: "a quantity its byte cannot hold", path: orders, schema: orderSchema, body: order(`[{"sku":"tea","quantity":300}]`, december, "true", porch),
			want: bodyRefused("body.items[0].quantity", "body.items[0].quantity holds 300, which is outside 0 to 255."),
		},
		{
			name: "a member written twice", path: orders, schema: orderSchema, body: order(tea, december, `true,"wrapped":false`, porch),
			want: bodyRefused("body.wrapped", "body.wrapped is written more than once."),
		},
		{
			name: "a fixed-length list one item short", path: orders, schema: orderSchema, body: order(tea, `["2026-12-01T09:00:00Z"]`, "true", porch),
			want: bodyRefused("body.deliver_between", "body.deliver_between must hold exactly 2 items, and it holds 1."),
		},
		{
			name: "a value of another JSON type", path: orders, schema: orderSchema, body: order(tea, december, `"yes"`, porch),
			want: bodyRefused("body.wrapped", `body.wrapped must hold true or false, and it holds \"yes\".`),
		},
		{
			name: "a key outside its set", path: orders, schema: orderSchema, body: order(tea, december, "true", `{"pigeon":"the roof"}`),
			want: bodyRefused(`body.courier_notes[\"pigeon\"]`, `body.courier_notes[\"pigeon\"] is under the key \"pigeon\", which is not one of the keys it accepts.`),
		},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			got := send(t, served, http.MethodPost, each.path, "", each.body)
			if got != each.want {
				t.Errorf("POST %s %s was answered\n%+v\nwant\n%+v", each.path, each.body, got, each.want)
			}
			if !jsontext.Value(each.body).IsValid() {
				return
			}
			value := mo.Fold(mo.TupleToResult(jsonschema.UnmarshalJSON(strings.NewReader(each.body))), func(read any) any { return read }, func(cause error) any {
				t.Fatalf("reading %s: %v", each.body, cause)
				return nil
			})
			taken := got.status/100 == 2
			mo.Fold(mo.EmptyableToOption(each.schema.Validate(value)), func(refused error) struct{} {
				if taken {
					t.Errorf("the published schema refuses %s, which the server takes with %d: %v", each.body, got.status, refused)
				}
				return struct{}{}
			}, func(error) struct{} {
				if !taken {
					t.Errorf("the published schema takes %s, which the server refuses with %d", each.body, got.status)
				}
				return struct{}{}
			})
		})
	}
}
