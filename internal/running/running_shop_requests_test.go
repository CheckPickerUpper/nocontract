package running_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/CheckPickerUpper/outcome"
)

type shopAnswer struct {
	status int
	allow  string
	body   string
}

type shopRequestCase struct {
	name       string
	method     string
	path       string
	shopper    string
	body       string
	want       shopAnswer
	bodySchema outcome.Option[shopBodySchema]
}

type shopBodySchema struct {
	path    string
	accepts bool
}

type shopBodyCase struct {
	name string
	body string
	want shopAnswer
}

type shopBodyRoute struct {
	path         string
	documentPath string
	cases        []shopBodyCase
}

type shopRefusal struct {
	Tag    string `json:"_tag"`
	Code   string `json:"code"`
	Part   string `json:"part"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type shopParameterProblem struct {
	part string
	name string
	read string
}

type shopBodyProblem struct {
	name   string
	reason string
}

const shopCanonicalDecimal = "is not a whole number written as canonical decimal text: an optional -, then 0 or a digit from 1 to 9 followed by any digits, and never -0."

const shopEmptyOrder = `{"items":[],"gift_note":null,"deliver_between":["2026-12-01T09:00:00Z","2026-12-02T09:00:00Z"],"wrapped":false,"tips":{},"courier_notes":{}}`

func shopParameterRefused(problem shopParameterProblem) shopAnswer {
	return shopRefused(shopRefusal{
		Tag: "RequestRefused", Code: "NC-1001", Part: problem.part, Name: problem.name,
		Reason: fmt.Sprintf("%q %s", problem.read, shopCanonicalDecimal),
	})
}

func shopBodyRefused(problem shopBodyProblem) shopAnswer {
	return shopRefused(shopRefusal{Tag: "RequestRefused", Code: "NC-1001", Part: "body", Name: problem.name, Reason: problem.reason})
}

func shopRefused(refusal shopRefusal) shopAnswer {
	return outcome.Fold(outcome.TupleToResult(json.Marshal(refusal)), func(content []byte) shopAnswer {
		return shopAnswer{status: 400, allow: "", body: string(content)}
	}, func(cause error) shopAnswer {
		return shopAnswer{status: 0, allow: "", body: cause.Error()}
	})
}

func shopRequests() []shopRequestCase {
	requests := []shopRequestCase{
		{
			name: "a shopper reads the balance", method: http.MethodGet, path: "/shoppers/7/balance?currency=CAD", shopper: "shopper-7", body: "",
			want: shopAnswer{status: 200, allow: "", body: `{"_tag":"BalanceRead","balance":{"cents":"1250","currency":"CAD"},"overdraft":null,` +
				`"changed":"2026-11-14T12:00:00Z","note":"first top-up"}`}, bodySchema: outcome.None[shopBodySchema](),
		},
		{
			name: "a path no route declares", method: http.MethodGet, path: "/shoppers/7/balances", shopper: "", body: "",
			want:       shopAnswer{status: 404, allow: "", body: `{"_tag":"RouteNotFound","code":"NC-1069","method":"GET","path":"/shoppers/7/balances"}`},
			bodySchema: outcome.None[shopBodySchema](),
		},
		{
			name: "a method no route at the path declares", method: http.MethodDelete, path: "/shoppers/7/balance", shopper: "", body: "",
			want:       shopAnswer{status: 405, allow: "GET", body: `{"_tag":"MethodNotDeclared","code":"NC-1070","method":"DELETE","allowed":["GET"]}`},
			bodySchema: outcome.None[shopBodySchema](),
		},
		{
			name: "an OPTIONS request to a declared path", method: http.MethodOptions, path: "/shoppers/7/top-ups", shopper: "", body: "",
			want: shopAnswer{status: 204, allow: "POST", body: ""}, bodySchema: outcome.None[shopBodySchema](),
		},
		{
			name: "the guard refuses a request without a shopper", method: http.MethodGet, path: "/shoppers/7/balance?currency=CAD", shopper: "", body: "",
			want: shopAnswer{status: 401, allow: "", body: `{"_tag":"NotSignedIn","code":"SH-1101","scope":"read-balance"}`}, bodySchema: outcome.None[shopBodySchema](),
		},
		{
			name: "the guard refuses an unverified shopper before reading the balance", method: http.MethodGet, path: "/shoppers/7/balance?currency=CAD", shopper: "shopper-8", body: "",
			want: shopAnswer{status: 401, allow: "", body: `{"_tag":"ShopperCredentialRejected","code":"SH-1102","scope":"read-balance"}`}, bodySchema: outcome.None[shopBodySchema](),
		},
		{
			name: "an admitted shopper has no ledger", method: http.MethodGet, path: "/shoppers/8/balance?currency=CAD", shopper: "shopper-7", body: "",
			want: shopAnswer{status: 404, allow: "", body: `{"_tag":"ShopperNotFound","code":"SH-1001","shopper_id":"8"}`}, bodySchema: outcome.None[shopBodySchema](),
		},
		{
			name: "the shopper has no balance in the requested currency", method: http.MethodGet, path: "/shoppers/7/balance?currency=USD", shopper: "shopper-7", body: "",
			want: shopAnswer{status: 404, allow: "", body: `{"_tag":"BalanceNotOpened","code":"SH-1003","currency":"USD"}`}, bodySchema: outcome.None[shopBodySchema](),
		},
	}
	for _, boundary := range []struct {
		name string
		text string
	}{
		{name: "a shopper at the signed 64-bit maximum", text: "9223372036854775807"},
		{name: "a shopper at the signed 64-bit minimum", text: "-9223372036854775808"},
	} {
		requests = append(requests, shopRequestCase{
			name: boundary.name, method: http.MethodGet, path: "/shoppers/" + boundary.text + "/balance?currency=CAD",
			shopper: "shopper-7", body: "", want: shopAnswer{status: 404, allow: "", body: `{"_tag":"ShopperNotFound","code":"SH-1001","shopper_id":"` + boundary.text + `"}`},
			bodySchema: outcome.None[shopBodySchema](),
		})
	}
	requests = append(requests, shopRequestCase{
		name: "a shopper above the signed 64-bit maximum", method: http.MethodGet,
		path: "/shoppers/9223372036854775808/balance?currency=CAD", shopper: "shopper-7", body: "",
		want: shopRefused(shopRefusal{
			Tag: "RequestRefused", Code: "NC-1001", Part: "path", Name: "shopper_id",
			Reason: `strconv.ParseInt: parsing "9223372036854775808": value out of range`,
		}), bodySchema: outcome.None[shopBodySchema](),
	})
	for _, boundary := range []struct {
		name string
		text string
	}{
		{name: "a store above the unsigned 16-bit maximum", text: "65536"},
		{name: "a negative store", text: "-1"},
	} {
		requests = append(requests, shopRequestCase{
			name: boundary.name, method: http.MethodPost, path: "/shoppers/7/orders?store=" + boundary.text,
			shopper: "shopper-7", body: shopEmptyOrder, want: shopRefused(shopRefusal{
				Tag: "RequestRefused", Code: "NC-1001", Part: "query", Name: "store",
				Reason: fmt.Sprintf("strconv.ParseUint: parsing %q: value out of range", boundary.text),
			}), bodySchema: outcome.None[shopBodySchema](),
		})
	}
	for _, parameter := range []struct {
		name string
		path string
		read string
	}{
		{name: "a 64-bit shopper with a leading zero", path: "/shoppers/007/balance?currency=CAD", read: "007"},
		{name: "a 64-bit shopper with a plus sign", path: "/shoppers/+7/balance?currency=CAD", read: "+7"},
		{name: "a 64-bit shopper of minus zero", path: "/shoppers/-0/balance?currency=CAD", read: "-0"},
	} {
		requests = append(requests, shopRequestCase{
			name: parameter.name, method: http.MethodGet, path: parameter.path, shopper: "shopper-7", body: "",
			want: shopParameterRefused(shopParameterProblem{part: "path", name: "shopper_id", read: parameter.read}), bodySchema: outcome.None[shopBodySchema](),
		})
	}
	for _, parameter := range []struct {
		name string
		path string
		read string
	}{
		{name: "a 16-bit store with a leading zero", path: "/shoppers/7/orders?store=007", read: "007"},
		{name: "a 16-bit store with a plus sign", path: "/shoppers/7/orders?store=%2B7", read: "+7"},
		{name: "a 16-bit store of minus zero", path: "/shoppers/7/orders?store=-0", read: "-0"},
	} {
		requests = append(requests, shopRequestCase{
			name: parameter.name, method: http.MethodPost, path: parameter.path, shopper: "shopper-7", body: shopEmptyOrder,
			want: shopParameterRefused(shopParameterProblem{part: "query", name: "store", read: parameter.read}), bodySchema: outcome.None[shopBodySchema](),
		})
	}
	for _, route := range shopBodyRoutes() {
		for _, body := range route.cases {
			requests = append(requests, shopRequestCase{
				name: body.name, method: http.MethodPost, path: route.path, shopper: "", body: body.body, want: body.want,
				bodySchema: outcome.Some(shopBodySchema{path: route.documentPath, accepts: body.want.status != http.StatusBadRequest}),
			})
		}
	}
	return requests
}

func shopBodyRoutes() []shopBodyRoute {
	queued := shopAnswer{status: 202, allow: "", body: `{"_tag":"TopUpQueued","reference":"CAD"}`}
	refusedAmount := shopAnswer{status: 409, allow: "", body: `{"_tag":"TopUpRefused","code":"SH-1002","reasons":["the amount is not above zero"]}`}
	topUpCases := []shopBodyCase{
		{name: "a whole top-up", body: `{"amount":{"cents":"500","currency":"CAD"}}`, want: queued},
		{name: "the maximum signed 64-bit amount as decimal text", body: `{"amount":{"cents":"9223372036854775807","currency":"CAD"}}`, want: queued},
		{name: "the minimum signed 64-bit amount reaches the domain", body: `{"amount":{"cents":"-9223372036854775808","currency":"CAD"}}`, want: refusedAmount},
		{name: "zero reaches the domain", body: `{"amount":{"cents":"0","currency":"CAD"}}`, want: refusedAmount},
		{name: "a member left out", body: `{"amount":{"cents":"500"}}`, want: shopBodyRefused(shopBodyProblem{
			name: "body.amount.currency", reason: "body.amount.currency is missing from the body.",
		})},
		{name: "a member the body does not declare", body: `{"amount":{"cents":"500","currency":"CAD"},"tip":5}`, want: shopBodyRefused(shopBodyProblem{
			name: "body.tip", reason: "body.tip is not a member the body accepts.",
		})},
		{name: "a value outside its set", body: `{"amount":{"cents":"500","currency":"EUR"}}`, want: shopBodyRefused(shopBodyProblem{
			name: "body.amount.currency", reason: `body.amount.currency holds "EUR", which is not one of the values it accepts.`,
		})},
		{name: "decimal text above its field's width", body: `{"amount":{"cents":"9223372036854775808","currency":"CAD"}}`, want: shopBodyRefused(shopBodyProblem{
			name: "body.amount.cents", reason: `body.amount.cents holds "9223372036854775808", which is outside -9223372036854775808 to 9223372036854775807.`,
		})},
		{name: "decimal text below its field's width", body: `{"amount":{"cents":"-9223372036854775809","currency":"CAD"}}`, want: shopBodyRefused(shopBodyProblem{
			name: "body.amount.cents", reason: `body.amount.cents holds "-9223372036854775809", which is outside -9223372036854775808 to 9223372036854775807.`,
		})},
		{name: "a JSON number where a 64-bit amount is decimal text", body: `{"amount":{"cents":500,"currency":"CAD"}}`, want: shopBodyRefused(shopBodyProblem{
			name: "body.amount.cents", reason: "body.amount.cents must hold a whole number written as a string, and it holds 500.",
		})},
		{name: "an empty body", body: "", want: shopBodyRefused(shopBodyProblem{name: "body", reason: "The body holds no JSON value."})},
		{name: "a null amount", body: `{"amount":null}`, want: shopBodyRefused(shopBodyProblem{name: "body.amount", reason: "body.amount must hold an object, and it holds null."})},
	}
	for _, decimal := range []struct {
		name string
		text string
	}{
		{name: "decimal text with a leading zero", text: "007"},
		{name: "decimal text with a plus sign", text: "+7"},
		{name: "decimal text of minus zero", text: "-0"},
	} {
		topUpCases = append(topUpCases, shopBodyCase{
			name: decimal.name, body: `{"amount":{"cents":"` + decimal.text + `","currency":"CAD"}}`,
			want: shopBodyRefused(shopBodyProblem{name: "body.amount.cents", reason: fmt.Sprintf("body.amount.cents holds %q, which %s", decimal.text, shopCanonicalDecimal)}),
		})
	}
	const tea = `[{"sku":"tea","quantity":2,"included":[{"sku":"strainer","quantity":1,"included":[]}]}]`
	const december = `["2026-12-01T10:00:00+01:00","2026-12-02T09:00:00Z"]`
	const porch = `{"drone":"the porch"}`
	const placed = `{"_tag":"OrderPlaced","order":{"items":` + tea + `,"gift_note":null,"deliver_between":["2026-12-01T09:00:00Z","2026-12-02T09:00:00Z"],"wrapped":true,"tips":{"CAD":2.5},"courier_notes":{"drone":"the porch"}}}`
	wholeOrder := shopOrderBody(shopOrderParts{items: tea, between: december, wrapped: "true", courierNotes: porch})
	orderCases := []shopBodyCase{
		{name: "a whole typed nested order", body: wholeOrder, want: shopAnswer{status: 201, allow: "", body: placed}},
		{name: "a small JSON integer accepts numeric minus zero", body: strings.Replace(wholeOrder, `"quantity":2`, `"quantity":-0`, 1), want: shopAnswer{status: 201, allow: "", body: strings.Replace(placed, `"quantity":2`, `"quantity":0`, 1)}},
		{name: "an empty typed order", body: shopEmptyOrder, want: shopAnswer{status: 201, allow: "", body: `{"_tag":"OrderPlaced","order":` + shopEmptyOrder + `}`}},
		{
			name: "a nested quantity its byte cannot hold", body: shopOrderBody(shopOrderParts{items: `[{"sku":"tea","quantity":2,"included":[{"sku":"strainer","quantity":300,"included":[]}]}]`, between: december, wrapped: "true", courierNotes: porch}),
			want: shopBodyRefused(shopBodyProblem{name: "body.items[0].included[0].quantity", reason: "body.items[0].included[0].quantity holds 300, which is outside 0 to 255."}),
		},
		{
			name: "a member written twice", body: shopOrderBody(shopOrderParts{items: tea, between: december, wrapped: `true,"wrapped":false`, courierNotes: porch}),
			want: shopBodyRefused(shopBodyProblem{name: "body.wrapped", reason: "body.wrapped is written more than once."}),
		},
		{
			name: "a fixed-length list one item short", body: shopOrderBody(shopOrderParts{items: tea, between: `["2026-12-01T09:00:00Z"]`, wrapped: "true", courierNotes: porch}),
			want: shopBodyRefused(shopBodyProblem{name: "body.deliver_between", reason: "body.deliver_between must hold exactly 2 items, and it holds 1."}),
		},
		{
			name: "a fixed-length list one item long", body: shopOrderBody(shopOrderParts{items: tea, between: `["2026-12-01T09:00:00Z","2026-12-02T09:00:00Z","2026-12-03T09:00:00Z"]`, wrapped: "true", courierNotes: porch}),
			want: shopBodyRefused(shopBodyProblem{name: "body.deliver_between", reason: "body.deliver_between must hold exactly 2 items, and it holds 3."}),
		},
		{
			name: "a value of another JSON type", body: shopOrderBody(shopOrderParts{items: tea, between: december, wrapped: `"yes"`, courierNotes: porch}),
			want: shopBodyRefused(shopBodyProblem{name: "body.wrapped", reason: `body.wrapped must hold true or false, and it holds "yes".`}),
		},
		{
			name: "a key outside its set", body: shopOrderBody(shopOrderParts{items: tea, between: december, wrapped: "true", courierNotes: `{"pigeon":"the roof"}`}),
			want: shopBodyRefused(shopBodyProblem{name: `body.courier_notes["pigeon"]`, reason: `body.courier_notes["pigeon"] is under the key "pigeon", which is not one of the keys it accepts.`}),
		},
		{
			name: "a nullable gift note accepts text", body: strings.Replace(wholeOrder, `"gift_note":null`, `"gift_note":"for Ada"`, 1),
			want: shopAnswer{status: 201, allow: "", body: strings.Replace(placed, `"gift_note":null`, `"gift_note":"for Ada"`, 1)},
		},
		{
			name: "a nullable member is still required", body: strings.Replace(wholeOrder, `"gift_note":null,`, "", 1),
			want: shopBodyRefused(shopBodyProblem{name: "body.gift_note", reason: "body.gift_note is missing from the body."}),
		},
	}
	return []shopBodyRoute{
		{path: "/shoppers/7/top-ups", documentPath: "/shoppers/{shopper_id}/top-ups", cases: topUpCases},
		{path: "/shoppers/7/orders?store=3", documentPath: "/shoppers/{shopper_id}/orders", cases: orderCases},
	}
}

type shopOrderParts struct {
	items        string
	between      string
	wrapped      string
	courierNotes string
}

func shopOrderBody(parts shopOrderParts) string {
	return `{"items":` + parts.items + `,"gift_note":null,"deliver_between":` + parts.between + `,"wrapped":` + parts.wrapped +
		`,"tips":{"CAD":2.5},"courier_notes":` + parts.courierNotes + `}`
}
