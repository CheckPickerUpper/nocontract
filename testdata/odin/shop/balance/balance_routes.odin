package balance

import "core:slice"

// Currency is a currency a balance is held in.
Currency :: enum {
	CAD,
	USD,
}

// Instant is an RFC 3339 timestamp.
//nocontract:instant
Instant :: distinct string

// Money is an amount in one currency.
Money :: struct #all_or_none {
	cents:    i64,
	currency: Currency,
}

// ReadInput names the shopper and the currency to read the balance in.
ReadInput :: struct #all_or_none {
	shopper_id: i64,
	currency:   Currency,
}

// BalanceRead is the shopper's balance and its last change.
//nocontract:status 200
@(not_an_error)
BalanceRead :: struct #all_or_none {
	balance:   Money,
	overdraft: Maybe(Money),
	changed:   Instant,
	note:      Maybe(string),
}

// ShopperNotFound is a shopper the shop holds no balance for.
//nocontract:status 404
@(error_code = "SH-1001")
ShopperNotFound :: struct #all_or_none {
	shopper_id: i64,
}

// BalanceNotOpened is a currency the shopper holds no balance in.
//nocontract:status 404
@(error_code = "SH-1003")
BalanceNotOpened :: struct #all_or_none {
	currency: Currency,
}

// ReadOutcome is what reading a balance ends as.
ReadOutcome :: union #no_nil {
	BalanceRead,
	ShopperNotFound,
	BalanceNotOpened,
}

// ReadBalance reads the example shopper's balance.
//nocontract:route GET /shoppers/{shopper_id}/balance
//nocontract:guard shopperauth.Check read-balance
@(require_results)
ReadBalance :: proc(input: ReadInput) -> ReadOutcome {
	if input.shopper_id != 7 {
		return ShopperNotFound{shopper_id = input.shopper_id}
	}
	if input.currency != .CAD {
		return BalanceNotOpened{currency = input.currency}
	}
	return BalanceRead{
		balance   = Money{cents = 1250, currency = input.currency},
		overdraft = {},
		changed   = Instant("2026-11-14T12:00:00Z"),
		note      = "first top-up",
	}
}

// TopUpBody carries the amount to add.
TopUpBody :: struct #all_or_none {
	amount: Money,
}

// TopUpInput names the shopper and carries the amount to add.
TopUpInput :: struct #all_or_none {
	shopper_id: i64,
	body:       TopUpBody,
}

// TopUpQueued is a top-up the shop will apply, under its reference.
//nocontract:status 202
@(not_an_error)
TopUpQueued :: struct #all_or_none {
	reference: string,
}

// TopUpRefused is a top-up the shop will not apply, with every reason.
//nocontract:status 409
@(error_code = "SH-1002")
TopUpRefused :: struct #all_or_none {
	reasons: []string,
}

// TopUpOutcome is what asking for a top-up ends as.
TopUpOutcome :: union #no_nil {
	TopUpQueued,
	TopUpRefused,
	ShopperNotFound,
}

// TopUp asks for an amount to be added to a shopper's balance.
//nocontract:route POST /shoppers/{shopper_id}/top-ups
@(require_results)
TopUp :: proc(input: TopUpInput) -> TopUpOutcome {
	if input.body.amount.cents <= 0 {
		return TopUpRefused{reasons = slice.clone([]string{"the amount is not above zero"})}
	}
	switch input.body.amount.currency {
	case .CAD:
		return TopUpQueued{reference = "CAD"}
	case .USD:
		return TopUpQueued{reference = "USD"}
	}
	unreachable()
}
