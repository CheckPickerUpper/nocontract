package nocontract_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samber/mo"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

// badgeRefusal is how the badge guard refuses a request, as a project declares a guard's refusal.
type badgeRefusal interface{ isBadgeRefusal() }

// badgeMissing is a request carrying no badge, for the route's room.
type badgeMissing struct {
	Room string `json:"room"`
}

func (badgeMissing) isBadgeRefusal() {}

// answerBadgeRefusal answers each refusal as the function NoContract generates into the refusal's
// package does.
func answerBadgeRefusal(refusal badgeRefusal) nocontract.Answer {
	switch member := refusal.(type) {
	case badgeMissing:
		return nocontract.Failed(nocontract.Failure{Status: http.StatusUnauthorized, Tag: "BadgeMissing", Code: "BG-1001", Fields: mo.TupleToResult(json.Marshal(member))})
	case nil:
	}
	return nocontract.NoOutcome()
}

// badgeCheck admits a request carrying a badge header and refuses every other for the room its
// route's guard line names.
type badgeCheck struct{}

func (badgeCheck) CheckRoute(route nocontract.GuardedRoute) mo.Result[nocontract.RequestCheck[badgeRefusal]] {
	return mo.Ok[nocontract.RequestCheck[badgeRefusal]](func(request *http.Request) nocontract.Decision[badgeRefusal] {
		if request.Header.Get("X-Badge") == "" {
			return nocontract.Refused[badgeRefusal]{Refusal: badgeMissing{Room: route.Argument}}
		}
		return nocontract.Admitted[badgeRefusal]{}
	})
}

// roomUnknown is why the closed guard cannot check a route: the room its guard line names.
type roomUnknown struct{ Room string }

func (unknown roomUnknown) Error() string { return "the guard knows no room " + unknown.Room }

// closedCheck cannot check any route.
type closedCheck struct{}

func (closedCheck) CheckRoute(route nocontract.GuardedRoute) mo.Result[nocontract.RequestCheck[badgeRefusal]] {
	return mo.Err[nocontract.RequestCheck[badgeRefusal]](roomUnknown{Room: route.Argument})
}

// opened is the route's own answer, which only an admitted request reaches.
func opened(*http.Request) nocontract.Answer {
	return nocontract.Succeeded(nocontract.Success{Status: http.StatusOK, Tag: "DoorOpened", Fields: mo.Ok([]byte(`{}`))})
}

// A request without a badge is answered with the guard's refusal, its status and its body, and a
// request with one reaches the route.
func TestAGuardRefusesWithItsRefusalAndAdmitsToTheRoute(t *testing.T) {
	t.Parallel()
	guarded := nocontract.GuardedBy[badgeRefusal](badgeCheck{}, nocontract.GuardedRoute{Method: "POST", Path: "/doors/{door}", Argument: "lobby"}, answerBadgeRefusal, opened)
	route := mo.Fold(guarded, func(route nocontract.Route) nocontract.Route { return route }, func(cause error) nocontract.Route {
		t.Fatalf("the badge guard could not check the route: %v", cause)
		return nocontract.Route{}
	})
	for _, each := range []struct {
		name   string
		badge  string
		status int
		body   string
	}{
		{name: "no badge", badge: "", status: http.StatusUnauthorized, body: `{"_tag":"BadgeMissing","code":"BG-1001","room":"lobby"}`},
		{name: "a badge", badge: "B-7", status: http.StatusOK, body: `{"_tag":"DoorOpened"}`},
	} {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/doors/front", http.NoBody)
			request.Header.Set("X-Badge", each.badge)
			recorder := httptest.NewRecorder()
			route.ServeHTTP(recorder, request)
			if recorder.Code != each.status || recorder.Body.String() != each.body {
				t.Fatalf("answered %d %s; want %d %s", recorder.Code, recorder.Body.String(), each.status, each.body)
			}
		})
	}
}

// A guard that cannot check a route keeps the route from being built, with its reason.
func TestAGuardThatCannotCheckARouteKeepsItsReason(t *testing.T) {
	t.Parallel()
	guarded := nocontract.GuardedBy[badgeRefusal](closedCheck{}, nocontract.GuardedRoute{Method: "POST", Path: "/vault", Argument: "vault"}, answerBadgeRefusal, opened)
	mo.Fold(guarded, func(nocontract.Route) struct{} {
		t.Fatal("the route was built; want the guard's reason")
		return struct{}{}
	}, func(cause error) struct{} {
		want := roomUnknown{Room: "vault"}
		if unknown := (roomUnknown{Room: ""}); !errors.As(cause, &unknown) || unknown != want {
			t.Fatalf("the route was refused with %v; want %v", cause, want)
		}
		return struct{}{}
	})
}
