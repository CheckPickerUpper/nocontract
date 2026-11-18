package main

import (
	"net/http"
	"testing"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

// bodyRefused is the answer refusing a body at name, which reason explains, as JSON writes it.
func bodyRefused(name, reason string) received {
	return received{status: 400, allow: "", header: "", body: `{"_tag":"RequestRefused","code":"NC-1001","part":"body","name":"` + name + `","reason":"` + reason + `"}`}
}

func TestARouteReadsItsBodyWholeOrRefusesItWhereItIsWrong(t *testing.T) {
	served := shopServer(t, []nocontract.Layer{})
	const topUps, orders = "/shoppers/7/top-ups", "/shoppers/7/orders"
	order := func(items, deliverBetween, wrapped string) string {
		return `{"items":` + items + `,"gift_note":null,"deliver_between":` + deliverBetween + `,"wrapped":` + wrapped + `,"tips":{"CAD":2.5}}`
	}
	const tea, december = `[{"sku":"tea","quantity":2}]`, `["2026-12-01T10:00:00+01:00","2026-12-02T09:00:00Z"]`
	cases := []struct {
		name string
		path string
		body string
		want received
	}{
		{
			name: "a whole top-up", path: topUps, body: `{"amount":{"cents":500,"currency":"CAD"}}`,
			want: received{status: 202, allow: "", header: "", body: `{"_tag":"TopUpQueued","reference":"CAD"}`},
		},
		{
			name: "a member left out", path: topUps, body: `{"amount":{"cents":500}}`,
			want: bodyRefused("body.amount.currency", "body.amount.currency is missing from the body."),
		},
		{
			name: "a member the body does not declare", path: topUps, body: `{"amount":{"cents":500,"currency":"CAD"},"tip":5}`,
			want: bodyRefused("body.tip", "body.tip is not a member the body accepts."),
		},
		{
			name: "a value outside its set", path: topUps, body: `{"amount":{"cents":500,"currency":"EUR"}}`,
			want: bodyRefused("body.amount.currency", `body.amount.currency holds \"EUR\", which is not one of the values it accepts.`),
		},
		{
			name: "a number outside its field's width", path: topUps, body: `{"amount":{"cents":9223372036854775808,"currency":"CAD"}}`,
			want: bodyRefused("body.amount.cents", "body.amount.cents holds 9223372036854775808, which is outside -9223372036854775808 to 9223372036854775807."),
		},
		{
			name: "an empty body", path: topUps, body: "",
			want: bodyRefused("body", "The body holds no JSON value."),
		},
		{
			name: "a whole order", path: orders, body: order(tea, december, "true"),
			want: received{status: 201, allow: "", header: "", body: `{"_tag":"OrderPlaced","order":{"items":[{"sku":"tea","quantity":2}],"gift_note":null,` +
				`"deliver_between":["2026-12-01T09:00:00Z","2026-12-02T09:00:00Z"],"wrapped":true,"tips":{"CAD":2.5}}}`},
		},
		{
			name: "a quantity its byte cannot hold", path: orders, body: order(`[{"sku":"tea","quantity":300}]`, december, "true"),
			want: bodyRefused("body.items[0].quantity", "body.items[0].quantity holds 300, which is outside 0 to 255."),
		},
		{
			name: "a member written twice", path: orders, body: order(tea, december, `true,"wrapped":false`),
			want: bodyRefused("body.wrapped", "body.wrapped is written more than once."),
		},
		{
			name: "a fixed-length list one item short", path: orders, body: order(tea, `["2026-12-01T09:00:00Z"]`, "true"),
			want: bodyRefused("body.deliver_between", "body.deliver_between must hold exactly 2 items, and it holds 1."),
		},
		{
			name: "a value of another JSON type", path: orders, body: order(tea, december, `"yes"`),
			want: bodyRefused("body.wrapped", `body.wrapped must hold true or false, and it holds \"yes\".`),
		},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			if got := send(t, served, http.MethodPost, each.path, "", each.body); got != each.want {
				t.Errorf("POST %s %s was answered\n%+v\nwant\n%+v", each.path, each.body, got, each.want)
			}
		})
	}
}
