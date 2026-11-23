// Package shopperauth verifies credentials before admitting a request to a shopper's route.
package shopperauth

import (
	"context"
	"net/http"

	"github.com/samber/mo"

	nocontract "github.com/CheckPickerUpper/nocontract/go"

	"example.com/shop/shopperauth/shopperrefusal"
)

// ShopperHeader carries the request's shopper credential.
const ShopperHeader = "X-Shopper"

// Check is the Shopper guard, which verifies the request's credential for the route's scope.
type Check struct{}

// CheckRoute admits the example credential shopper-7 and refuses missing or unrecognized credentials.
func (Check) CheckRoute(route nocontract.GuardedRoute) mo.Result[nocontract.RequestCheck[shopperrefusal.Refusal]] {
	return mo.Ok[nocontract.RequestCheck[shopperrefusal.Refusal]](func(request *http.Request) nocontract.Decision[shopperrefusal.Refusal] {
		return nocontract.CheckSignIn(request, ShopperHeader,
			func() shopperrefusal.Refusal { return shopperrefusal.NotSignedIn{Scope: route.Argument} },
			func(_ context.Context, credential string) nocontract.Decision[shopperrefusal.Refusal] {
				if credential == "shopper-7" {
					return nocontract.Admitted[shopperrefusal.Refusal]{}
				}
				return nocontract.Refused[shopperrefusal.Refusal]{Refusal: shopperrefusal.ShopperCredentialRejected{Scope: route.Argument}}
			})
	})
}
