// Package nocontract is what the routes NoContract generates are built from. A route is a handler
// declared in Go:
//
//	// ReadBalance reads one shopper's balance.
//	//
//	//nocontract:route GET /shoppers/{shopper_id}/balance
//	func ReadBalance(ctx context.Context, input ReadInput) ReadOutcome
//
// whose outcome is a sealed interface and whose members each declare their status:
//
//	// ShopperNotFound is a shopper the shop holds no balance for.
//	//
//	//ergo:code SH-1001
//	//nocontract:status 404
//	type ShopperNotFound struct {
//		ShopperID int `json:"shopper_id"`
//	}
//
// The generator reads those declarations once and writes both the Go code that serves each
// route and the Effect HttpApi its TypeScript clients call.
package nocontract

import (
	"net/http"

	"github.com/samber/mo"
)

// Route is one served route: its method, its path in net/http's pattern syntax and what serves
// it, behind its guard when it names one. The generated Routes function of each package builds
// them.
type Route struct {
	method string
	path   string
	serve  http.Handler
}

// Pattern is the route's net/http ServeMux pattern, such as "GET /balance/{shopper_id}".
func (route Route) Pattern() string { return route.method + " " + route.path }

// ServeHTTP serves the request.
func (route Route) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	route.serve.ServeHTTP(writer, request)
}

// RouteAt is where a route is served: its method and its path in net/http's pattern syntax.
type RouteAt struct {
	Method string
	Path   string
}

// Served is the route at its place served by serve, behind no guard. Only generated code calls it.
func Served(at RouteAt, serve http.Handler) mo.Result[Route] {
	return mo.Ok(Route{method: at.Method, path: at.Path, serve: serve})
}

// GuardedRoute is the route a guard is asked to check: its method, its path and the argument its
// //nocontract:guard line gives the guard, such as the action a staff member must hold.
type GuardedRoute struct {
	Method   string
	Path     string
	Argument string
}

// Guard puts a check in front of every route that names it. Guarded is the handler a request to
// the route reaches, which runs serve only for a request the check admits and answers every
// other with a member of the guard's declared refusal; or why the guard cannot check the route.
type Guard interface {
	Guarded(route GuardedRoute, serve http.Handler) mo.Result[http.Handler]
}

// GuardedBy is the guarded route served by serve behind guard. Only generated code calls it.
func GuardedBy(guard Guard, guarded GuardedRoute, serve http.Handler) mo.Result[Route] {
	return mo.Fold(guard.Guarded(guarded, serve), func(checked http.Handler) mo.Result[Route] {
		return mo.Ok(Route{method: guarded.Method, path: guarded.Path, serve: checked})
	}, mo.Err[Route])
}

// Collect is every route, or the first reason a route could not be built.
func Collect(routes ...mo.Result[Route]) mo.Result[[]Route] {
	collected := mo.Ok([]Route{})
	for _, route := range routes {
		collected = mo.Fold(collected, func(built []Route) mo.Result[[]Route] {
			return mo.Fold(route, func(served Route) mo.Result[[]Route] { return mo.Ok(append(built, served)) }, mo.Err[[]Route])
		}, mo.Err[[]Route])
	}
	return collected
}

// NoInput is the input of a route that reads nothing from its request.
type NoInput struct{}
