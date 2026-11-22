// Command shop serves the example shop's routes on the address SHOP_ADDRESS names.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"

	nocontract "github.com/CheckPickerUpper/nocontract/go"

	"example.com/shop/balance"
	"example.com/shop/server"
	"example.com/shop/shopperauth"
)

func main() {
	serverContext := context.Background()
	built := server.New([]nocontract.Layer{logRequest}, balance.Ledger{ShopperID: 7, Cents: 1250, OverdraftCents: mo.None[int]()}, shopperauth.Check{})
	os.Exit(mo.Fold(built, func(api server.API) int { return serve(serverContext, api) }, func(cause error) int {
		fmt.Fprintln(os.Stderr, cause)
		return 1
	}))
}

// serve serves the API until the server stops, and is the exit code the stop gives.
func serve(serverContext context.Context, api server.API) int {
	return outcome.Fold(server.Listen(serverContext, nocontract.ListenOn(os.Getenv("SHOP_ADDRESS")), api), func(listening nocontract.ListeningServer) int {
		defer func() {
			outcome.Fold(listening.Stop(serverContext), func(nocontract.ServerStopped) struct{} { return struct{}{} }, func(cause error) struct{} {
				fmt.Fprintln(os.Stderr, cause)
				return struct{}{}
			})
		}()
		return outcome.Fold(outcome.TupleToResult(fmt.Fprintln(os.Stdout, listening.Address())), func(int) int {
			return outcome.Fold(listening.Wait(), func(nocontract.ServerStopped) int { return 0 }, func(cause error) int {
				fmt.Fprintln(os.Stderr, cause)
				return 1
			})
		}, func(cause error) int {
			fmt.Fprintln(os.Stderr, cause)
			return 1
		})
	}, func(cause error) int {
		fmt.Fprintln(os.Stderr, cause)
		return 1
	})
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
