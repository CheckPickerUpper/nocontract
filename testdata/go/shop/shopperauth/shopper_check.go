// Package shopperauth admits a request to a shopper's route only when it names a shopper.
package shopperauth

import (
	"net/http"

	"github.com/samber/mo"

	nocontract "github.com/CheckPickerUpper/nocontract/go"

	"example.com/shop/shopperauth/shopperrefusal"
)

// ShopperHeader names the shopper a request is sent for.
const ShopperHeader = "X-Shopper"

// Check is the Shopper guard: a request without a shopper is refused as NotSignedIn for the
// route's scope.
type Check struct{}

// Guarded admits a request naming a shopper to serve and refuses every other.
func (Check) Guarded(route nocontract.GuardedRoute, serve http.Handler) mo.Result[http.Handler] {
	return mo.Ok[http.Handler](http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get(ShopperHeader) == "" {
			nocontract.Write(writer, shopperrefusal.NocontractAnswerRefusal(shopperrefusal.NotSignedIn{Scope: route.Argument}))
			return
		}
		serve.ServeHTTP(writer, request)
	}))
}
