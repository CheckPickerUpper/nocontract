// Package nocontract is what the routes go-no-contract generates are built from. A route is a
// handler declared in Go:
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

import "net/http"

// Route is one served route: its method, its path in net/http's pattern syntax and what serves
// it. The generated Routes function of each package builds them.
type Route struct {
	method string
	path   string
	serve  http.HandlerFunc
}

// NewRoute is the route serving method and path with serve. Only generated code calls it.
func NewRoute(method, path string, serve http.HandlerFunc) Route {
	return Route{method: method, path: path, serve: serve}
}

// Pattern is the route's net/http ServeMux pattern, such as "GET /balance/{shopper_id}".
func (route Route) Pattern() string { return route.method + " " + route.path }

// ServeHTTP serves the request.
func (route Route) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	route.serve(writer, request)
}

// NoInput is the input of a route that reads nothing from its request.
type NoInput struct{}
