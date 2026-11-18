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

// CheckRoute checks every route: it admits a request naming a shopper and refuses every other.
func (Check) CheckRoute(route nocontract.GuardedRoute) mo.Result[nocontract.RequestCheck[shopperrefusal.Refusal]] {
	return mo.Ok[nocontract.RequestCheck[shopperrefusal.Refusal]](func(request *http.Request) nocontract.Decision[shopperrefusal.Refusal] {
		if request.Header.Get(ShopperHeader) == "" {
			return nocontract.Refused[shopperrefusal.Refusal]{Refusal: shopperrefusal.NotSignedIn{Scope: route.Argument}}
		}
		return nocontract.Admitted[shopperrefusal.Refusal]{}
	})
}
