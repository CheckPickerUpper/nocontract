// Command shop serves the example shop's routes on the address SHOP_ADDRESS names.
package main

import (
	"net/http"
	"os"

	"github.com/samber/mo"

	nocontract "github.com/CheckPickerUpper/nocontract/go"

	"example.com/shop/balance"
	"example.com/shop/shopperauth"
)

func main() {
	served := balance.Routes(balance.Serving{Ledger: balance.Ledger{ShopperID: 7, Cents: 1250}, Shopper: shopperauth.Check{}})
	mux := mo.Fold(served, func(routes []nocontract.Route) *http.ServeMux {
		mux := http.NewServeMux()
		for _, route := range routes {
			mux.Handle(route.Pattern(), route)
		}
		return mux
	}, func(cause error) *http.ServeMux { panic(cause) })
	server := &http.Server{Addr: os.Getenv("SHOP_ADDRESS"), Handler: mux, ReadHeaderTimeout: 0}
	mo.Fold(mo.EmptyableToOption(server.ListenAndServe()), func(cause error) struct{} { panic(cause) }, func(error) struct{} { return struct{}{} })
}
