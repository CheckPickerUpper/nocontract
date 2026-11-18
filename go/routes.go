// Package nocontract is what the routes NoContract generates are built from. A route is a handler
// declared in Go:
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
// route and the Effect HttpApi its TypeScript clients call. The only value that serves routes is
// the API type NoContract generates into the project's server package.
package nocontract

import (
	"net/http"

	"github.com/samber/mo"
)

// GuardedRoute is the route a guard is asked to check: its method, its path and the argument its
// //nocontract:guard line gives the guard, such as the action a staff member must hold.
type GuardedRoute struct {
	Method   string
	Path     string
	Argument string
}

// Guard puts a check in front of every route that names it. Refusal is the guard's sealed refusal,
// the type marked //nocontract:guard with the guard's name. CheckRoute is asked once for each
// route when the server is built: how the guard decides each request to that route, or why it
// cannot check the route at all.
type Guard[Refusal any] interface {
	CheckRoute(route GuardedRoute) mo.Result[RequestCheck[Refusal]]
}

// RequestCheck decides one request to the route a guard checks. It reads the request and never
// answers it: the generated code writes the refusal, or runs the route.
type RequestCheck[Refusal any] func(request *http.Request) Decision[Refusal]

// Decision is what a guard decided about one request: Admitted, or Refused with one member of the
// guard's own refusal. Its method names Refusal, so a guard's decision holds no other guard's
// refusal and no route's failure.
//
//sumtype:decl
type Decision[Refusal any] interface {
	isDecisionOf(refusal Refusal)
}

// Admitted is a request the guard lets through to its route.
//
//ergo:not-an-error
type Admitted[Refusal any] struct{}

// Refused is a request the guard turns away, answered with Refusal instead of its route.
//
//ergo:not-an-error
type Refused[Refusal any] struct {
	Refusal Refusal
}

func (Admitted[Refusal]) isDecisionOf(Refusal) {}
func (Refused[Refusal]) isDecisionOf(Refusal)  {}

// Decided is the answer to a request a guard decided: the route's own answer when admitted, the
// refusal's answer when refused, and AnswerNotWritten when the guard decided nothing.
func Decided[Refusal any](decision Decision[Refusal], refused func(refusal Refusal) Answer, admitted func() Answer) Answer {
	switch decided := decision.(type) {
	case Admitted[Refusal]:
		return admitted()
	case Refused[Refusal]:
		return refused(decided.Refusal)
	case nil:
	}
	return NocontractAnswerSharedFailure(AnswerNotWritten{Answer: "", Reason: "the guard decided nothing"})
}

// NoInput is the input of a route that reads nothing from its request.
type NoInput struct{}
