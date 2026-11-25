// Command shop serves the example shop's routes on the address SHOP_ADDRESS names.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"
	"golang.org/x/sync/errgroup"

	nocontract "github.com/CheckPickerUpper/nocontract/go"

	"example.com/shop/balance"
	"example.com/shop/server"
	"example.com/shop/shopperauth"
)

func main() {
	serverContext := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	built := server.New([]nocontract.Layer{observeRequest("outer", logger), observeRequest("inner", logger)}, balance.Ledger{ShopperID: 7, Cents: 1250, OverdraftCents: outcome.None[int]()}, shopperauth.Check{})
	os.Exit(mo.Fold(built, func(api server.API) int { return serve(serverContext, api, logger) }, func(cause error) int {
		fmt.Fprintln(os.Stderr, cause)
		return 1
	}))
}

// serve registers shutdown signals before publishing readiness and drains requests before returning.
func serve(serverContext context.Context, api server.API, logger *slog.Logger) int {
	shutdownRequested, requestShutdown := signal.NotifyContext(serverContext, os.Interrupt, syscall.SIGTERM)
	defer requestShutdown()
	configured := outcome.Fold(nocontract.ParseRequestBodyLimit(1024), func(limit nocontract.RequestBodyLimit) outcome.Result[nocontract.ListenOptions] {
		options := nocontract.WithRequestLogger(nocontract.WithRequestBodyLimit(nocontract.ListenOn(os.Getenv("SHOP_ADDRESS")), limit), logger)
		if os.Getenv("SHOP_TEST_REQUEST_DEADLINE") == "1" {
			return nocontract.WithRequestDeadline(options, 50)
		}
		return outcome.Ok(options)
	}, outcome.Err[nocontract.ListenOptions])
	started := outcome.Fold(configured, func(options nocontract.ListenOptions) outcome.Result[nocontract.ListeningServer] {
		return server.Listen(serverContext, options, api)
	}, outcome.Err[nocontract.ListeningServer])
	return outcome.Fold(started, func(listening nocontract.ListeningServer) int {
		stopping := &errgroup.Group{}
		stopping.Go(func() error {
			<-shutdownRequested.Done()
			// Request contexts stay live while Stop closes the listener and drains them.
			shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(serverContext), 5*time.Second)
			defer cancel()
			return outcome.Fold(listening.Stop(shutdownContext), func(nocontract.ServerStopped) error { return nil }, func(cause error) error {
				return cause
			})
		})
		served := outcome.Fold(outcome.TupleToResult(fmt.Fprintf(os.Stdout, "Listening on http://%s\n", listening.Address())), func(int) outcome.Result[nocontract.ServerStopped] {
			return listening.Wait()
		}, outcome.Err[nocontract.ServerStopped])
		requestShutdown()
		shutdownFailure := stopping.Wait()
		return outcome.Fold(served, func(nocontract.ServerStopped) int {
			return outcome.FoldOption(outcome.EmptyableToOption(shutdownFailure), func(cause error) int {
				fmt.Fprintln(os.Stderr, cause)
				return 1
			}, func() int { return 0 })
		}, func(cause error) int {
			fmt.Fprintln(os.Stderr, cause)
			return 1
		})
	}, func(cause error) int {
		fmt.Fprintln(os.Stderr, cause)
		return 1
	})
}
