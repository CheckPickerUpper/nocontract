// Command shop serves the example shop's routes on the address SHOP_ADDRESS names.
package main

import (
	"net/http"
	"os"

	"example.com/shop/balance"
)

func main() {
	mux := http.NewServeMux()
	for _, route := range balance.Routes() {
		mux.Handle(route.Pattern(), route)
	}
	server := &http.Server{Addr: os.Getenv("SHOP_ADDRESS"), Handler: mux, ReadHeaderTimeout: 0}
	if served := server.ListenAndServe(); served != nil {
		panic(served)
	}
}
