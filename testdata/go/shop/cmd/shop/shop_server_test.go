package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samber/mo"

	nocontract "github.com/CheckPickerUpper/nocontract/go"

	"example.com/shop/balance"
	"example.com/shop/server"
	"example.com/shop/shopperauth"
	"example.com/shop/shopperauth/shopperrefusal"
)

// shopServer serves the shop's generated API through layers, stopping the test when it cannot be
// built.
func shopServer(t *testing.T, layers []nocontract.Layer) *httptest.Server {
	t.Helper()
	return ledgerServer(t, balance.Ledger{ShopperID: 7, Cents: 1250, OverdraftCents: mo.None[int]()}, layers)
}

// ledgerServer serves the shop's generated API over ledger through layers, stopping the test when it
// cannot be built.
func ledgerServer(t *testing.T, ledger balance.Ledger, layers []nocontract.Layer) *httptest.Server {
	t.Helper()
	built := server.New(layers, ledger, shopperauth.Check{})
	served := httptest.NewServer(mo.Fold(built, func(api server.API) server.API { return api }, func(cause error) server.API {
		t.Fatalf("building the shop's API: %v", cause)
		return server.API{}
	}))
	t.Cleanup(served.Close)
	return served
}

// received is a response as the client read it.
type received struct {
	status int
	allow  string
	header string
	body   string
}

// send sends one request for shopper, an empty shopper meaning none, carrying body, and reads the
// response.
func send(t *testing.T, served *httptest.Server, method, path, shopper, body string) received {
	t.Helper()
	failed := func(doing string) func(error) received {
		return func(cause error) received {
			t.Fatalf("%s %s %s: %v", doing, method, path, cause)
			return received{status: 0, allow: "", header: "", body: ""}
		}
	}
	built := mo.TupleToResult(http.NewRequestWithContext(t.Context(), method, served.URL+path, strings.NewReader(body)))
	return mo.Fold(built, func(request *http.Request) received {
		request.Header.Set(shopperauth.ShopperHeader, shopper)
		return mo.Fold(mo.TupleToResult(served.Client().Do(request)), func(response *http.Response) received {
			return mo.Fold(mo.TupleToResult(io.ReadAll(response.Body)), func(body []byte) received {
				answer := received{status: response.StatusCode, allow: response.Header.Get("Allow"), header: response.Header.Get("X-Request-Seen"), body: string(body)}
				return mo.Fold(mo.EmptyableToOption(response.Body.Close()), failed("closing the answer to"), func(error) received { return answer })
			}, failed("reading the answer to"))
		}, failed("sending"))
	}, failed("building"))
}

// A balance of 2^53 + 1 cents, the least whole number a JavaScript number cannot hold, is answered as
// the string of its decimal text, which a client reads whole, and so is one an option holds; an
// empty option is null.
func TestAWholeNumberPastWhatJavaScriptHoldsIsAnsweredAsDecimalText(t *testing.T) {
	answered := func(overdraft string) received {
		return received{status: 200, allow: "", header: "", body: `{"_tag":"BalanceRead","balance":{"cents":"9007199254740993","currency":"CAD"},` +
			`"overdraft":` + overdraft + `,"changed":"2026-11-14T12:00:00Z","note":"first top-up"}`}
	}
	for _, each := range []struct {
		name      string
		overdraft mo.Option[int]
		want      received
	}{
		{name: "an overdraft", overdraft: mo.Some(1<<53 + 3), want: answered(`{"cents":"9007199254740995","currency":"CAD"}`)},
		{name: "no overdraft", overdraft: mo.None[int](), want: answered("null")},
	} {
		t.Run(each.name, func(t *testing.T) {
			served := ledgerServer(t, balance.Ledger{ShopperID: 7, Cents: 1<<53 + 1, OverdraftCents: each.overdraft}, []nocontract.Layer{})
			if got := send(t, served, http.MethodGet, "/shoppers/7/balance?currency=CAD", "shopper-7", ""); got != each.want {
				t.Errorf("the balance was answered %+v; want %+v", got, each.want)
			}
		})
	}
}

// scopeUnknown is why closedGuard cannot check a route: the scope its guard line names.
type scopeUnknown struct{ Scope string }

func (unknown scopeUnknown) Error() string { return "the guard knows no scope " + unknown.Scope }

// closedGuard cannot check any route.
type closedGuard struct{}

func (closedGuard) CheckRoute(route nocontract.GuardedRoute) mo.Result[nocontract.RequestCheck[shopperrefusal.Refusal]] {
	return mo.Err[nocontract.RequestCheck[shopperrefusal.Refusal]](scopeUnknown{Scope: route.Argument})
}

func TestAGuardThatCannotCheckARouteKeepsTheAPIFromBeingBuiltWithItsReason(t *testing.T) {
	built := server.New([]nocontract.Layer{}, balance.Ledger{ShopperID: 7, Cents: 1250, OverdraftCents: mo.None[int]()}, closedGuard{})
	mo.Fold(built, func(server.API) struct{} {
		t.Fatal("the API was built; want the guard's reason")
		return struct{}{}
	}, func(cause error) struct{} {
		want := scopeUnknown{Scope: "read-balance"}
		if unknown := (scopeUnknown{Scope: ""}); !errors.As(cause, &unknown) || unknown != want {
			t.Fatalf("building the API ended with %v; want %v", cause, want)
		}
		return struct{}{}
	})
}

// seen is one request as a layer saw it answered.
type seen struct {
	route  string
	status int
	code   string
}

func TestALayerSeesEveryAnswerAndSetsAHeaderTheClientReceives(t *testing.T) {
	visits := []seen{}
	recording := func(visit nocontract.Visit, next func() nocontract.Answered) nocontract.Answered {
		visit.ResponseHeader().Set("X-Request-Seen", "yes")
		answered := next()
		route := "no route"
		switch matched := visit.Route().(type) {
		case nocontract.RouteMatched:
			route = matched.Method + " " + matched.Path
		case nocontract.NoRouteMatched, nil:
		}
		code := mo.Fold(answered.Code(), func(code string) string { return code }, func(error) string { return "" })
		visits = append(visits, seen{route: route, status: answered.Status(), code: code})
		return answered
	}
	served := shopServer(t, []nocontract.Layer{recording})
	answers := []received{
		send(t, served, http.MethodGet, "/shoppers/7/balance?currency=CAD", "shopper-7", ""),
		send(t, served, http.MethodGet, "/shoppers/7/balance?currency=CAD", "", ""),
		send(t, served, http.MethodGet, "/shoppers", "", ""),
	}
	for _, answer := range answers {
		if answer.header != "yes" {
			t.Errorf("an answer %+v came without the layer's header", answer)
		}
	}
	want := []seen{
		{route: "GET /shoppers/{shopper_id}/balance", status: 200, code: ""},
		{route: "GET /shoppers/{shopper_id}/balance", status: 401, code: "SH-1101"},
		{route: "no route", status: 404, code: "NC-1069"},
	}
	if len(visits) != len(want) {
		t.Fatalf("the layer saw %+v; want %+v", visits, want)
	}
	for i := range want {
		if visits[i] != want[i] {
			t.Errorf("the layer saw request %d as %+v; want %+v", i, visits[i], want[i])
		}
	}
}
