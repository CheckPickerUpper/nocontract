// Package balance serves shoppers' balances.
package balance

import (
	"context"
	"time"

	"github.com/samber/mo"
)

// Currency is a currency a balance is held in.
type Currency string

// The currencies a balance is held in.
const (
	CAD Currency = "CAD"
	USD Currency = "USD"
)

// Money is an amount in one currency.
type Money struct {
	Cents    int      `json:"cents"`
	Currency Currency `json:"currency"`
}

// ReadInput names the shopper and the currency to read the balance in.
type ReadInput struct {
	ShopperID int      `path:"shopper_id"`
	Currency  Currency `query:"currency"`
}

// ReadOutcome is what reading a balance ended as.
//
//sumtype:decl
type ReadOutcome interface {
	isReadOutcome()
}

// BalanceRead is the shopper's balance, when it last changed, and staff's note on it.
//
//ergo:not-an-error
//nocontract:status 200
type BalanceRead struct {
	Balance Money             `json:"balance"`
	Changed time.Time         `json:"changed"`
	Note    mo.Option[string] `json:"note"`
}

// ShopperNotFound is a shopper the shop holds no balance for.
//
//ergo:code SH-1001
//nocontract:status 404
type ShopperNotFound struct {
	ShopperID int `json:"shopper_id"`
}

// BalanceNotOpened is a currency the shopper holds no balance in.
//
//ergo:code SH-1003
//nocontract:status 404
type BalanceNotOpened struct {
	Currency Currency `json:"currency"`
}

func (BalanceRead) isReadOutcome()      {}
func (ShopperNotFound) isReadOutcome()  {}
func (BalanceNotOpened) isReadOutcome() {}

// Ledger holds the one shopper whose balance the example shop keeps, in Canadian dollars.
type Ledger struct {
	ShopperID int
	Cents     int
}

// ReadBalance reads one shopper's balance, for a request the Shopper guard admitted.
//
//nocontract:route GET /shoppers/{shopper_id}/balance
//nocontract:guard Shopper read-balance
func (ledger Ledger) ReadBalance(_ context.Context, input ReadInput) ReadOutcome {
	if input.ShopperID != ledger.ShopperID {
		return ShopperNotFound{ShopperID: input.ShopperID}
	}
	if input.Currency != CAD {
		return BalanceNotOpened{Currency: input.Currency}
	}
	return BalanceRead{
		Balance: Money{Cents: ledger.Cents, Currency: input.Currency},
		Changed: time.Date(2026, time.November, 14, 12, 0, 0, 0, time.UTC),
		Note:    mo.Some("first top-up"),
	}
}

// TopUpInput names the shopper and carries the amount to add.
type TopUpInput struct {
	ShopperID int `path:"shopper_id"`
	Body      struct {
		Amount Money `json:"amount"`
	}
}

// TopUpOutcome is what asking for a top-up ended as.
//
//sumtype:decl
type TopUpOutcome interface {
	isTopUpOutcome()
}

// TopUpQueued is a top-up the shop will apply, under its reference.
//
//ergo:not-an-error
//nocontract:status 202
type TopUpQueued struct {
	Reference string `json:"reference"`
}

// TopUpRefused is a top-up the shop will not apply, with every reason.
//
//ergo:code SH-1002
//nocontract:status 409
type TopUpRefused struct {
	Reasons []string `json:"reasons"`
}

func (TopUpQueued) isTopUpOutcome()     {}
func (TopUpRefused) isTopUpOutcome()    {}
func (ShopperNotFound) isTopUpOutcome() {}

// TopUp asks for an amount to be added to a shopper's balance.
//
//nocontract:route POST /shoppers/{shopper_id}/top-ups
func TopUp(_ context.Context, input TopUpInput) TopUpOutcome {
	if input.Body.Amount.Cents <= 0 {
		return TopUpRefused{Reasons: []string{"the amount is not above zero"}}
	}
	return TopUpQueued{Reference: string(input.Body.Amount.Currency)}
}
