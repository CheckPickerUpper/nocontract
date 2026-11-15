// Package shopperrefusal declares how the shopper check refuses a request before its route runs.
package shopperrefusal

// Refusal is how the shopper check refuses a request.
//
//sumtype:decl
//nocontract:guard Shopper
type Refusal interface {
	isRefusal()
}

// NotSignedIn is a request carrying no shopper, to a route only a shopper may read.
//
//ergo:code SH-1101
//nocontract:status 401
type NotSignedIn struct {
	Scope string `json:"scope"`
}

func (NotSignedIn) isRefusal() {}
