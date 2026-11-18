// Command shop serves the example shop's routes on the address SHOP_ADDRESS names.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/samber/mo"

	nocontract "github.com/CheckPickerUpper/nocontract/go"

	"example.com/shop/balance"
	"example.com/shop/server"
	"example.com/shop/shopperauth"
)

func main() {
	built := server.New([]nocontract.Layer{logRequest}, balance.Ledger{ShopperID: 7, Cents: 1250, OverdraftCents: mo.None[int]()}, shopperauth.Check{})
	os.Exit(mo.Fold(built, serve, func(cause error) int {
		fmt.Fprintln(os.Stderr, cause)
		return 1
	}))
}

// serve serves the API until the server stops, and is the exit code the stop gives.
func serve(api server.API) int {
	listening := &http.Server{
		Addr: os.Getenv("SHOP_ADDRESS"), Handler: api, DisableGeneralOptionsHandler: true, TLSConfig: nil,
		ReadTimeout: 0, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 0, IdleTimeout: 0, MaxHeaderBytes: 0,
		MaxHeaderValueCount: 0, TLSNextProto: nil, ConnState: nil, ErrorLog: nil, BaseContext: nil, ConnContext: nil,
		HTTP2: nil, Protocols: nil, DisableClientPriority: false,
	}
	// ListenAndServe always returns the error that stopped it.
	fmt.Fprintln(os.Stderr, listening.ListenAndServe())
	return 1
}

// logRequest writes each request's method, the route it matched, and the status and code it was
// answered with to stderr.
func logRequest(visit nocontract.Visit, next func() nocontract.Answered) nocontract.Answered {
	answered := next()
	route := "no route"
	switch matched := visit.Route().(type) {
	case nocontract.RouteMatched:
		route = matched.Method + " " + matched.Path
	case nocontract.NoRouteMatched, nil:
	}
	code := mo.Fold(answered.Code(), func(code string) string { return code }, func(error) string { return "no code" })
	fmt.Fprintf(os.Stderr, "%s %s: %s %d %s\n", visit.Method(), visit.URL().Path, route, answered.Status(), code)
	return answered
}
