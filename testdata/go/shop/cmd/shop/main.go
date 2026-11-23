// Command shop serves the example shop's routes on the address SHOP_ADDRESS names.
package main

import (
	"context"
	"fmt"
	"log/slog"
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
	built := server.New([]nocontract.Layer{}, balance.Ledger{ShopperID: 7, Cents: 1250, OverdraftCents: mo.None[int]()}, shopperauth.Check{})
	os.Exit(mo.Fold(built, func(api server.API) int { return serve(serverContext, api) }, func(cause error) int {
		fmt.Fprintln(os.Stderr, cause)
		return 1
	}))
}

// serve serves the API until the server stops, and is the exit code the stop gives.
func serve(serverContext context.Context, api server.API) int {
	options := nocontract.WithRequestLogger(nocontract.ListenOn(os.Getenv("SHOP_ADDRESS")), slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	return outcome.Fold(server.Listen(serverContext, options, api), func(listening nocontract.ListeningServer) int {
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
