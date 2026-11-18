package routes

import (
	"context"

	"example.com/refused/elsewhere"
)

// BodyOutcome is what reading a body another package declares ended as.
//
//sumtype:decl
type BodyOutcome interface {
	isBodyOutcome()
}

// BodyRead is a body read whole.
//
//ergo:not-an-error
//nocontract:status 200
type BodyRead struct{}

func (BodyRead) isBodyOutcome() {}

// ReadPlaced reads a body whose struct holds a field only its own package can write.
//
//nocontract:route POST /placed
func ReadPlaced(context.Context, elsewhere.PlacedInput) BodyOutcome { return BodyRead{} }

// ReadHidden reads a body of a type its own package does not export.
//
//nocontract:route POST /hidden
func ReadHidden(context.Context, elsewhere.HiddenInput) BodyOutcome { return BodyRead{} }
