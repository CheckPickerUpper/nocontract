package routes

import (
	"context"

	nocontract "github.com/CheckPickerUpper/nocontract/go"

	"example.com/refused/elsewhere"
)

// TallyOutcome is what reading a tally another package declares ended as.
//
//sumtype:decl
type TallyOutcome interface {
	isTallyOutcome()
}

// TallyRead is the tally, whose count's type the generated code here must name to write it as
// decimal text.
//
//ergo:not-an-error
//nocontract:status 200
type TallyRead struct {
	Tally elsewhere.Tally `json:"tally"`
}

func (TallyRead) isTallyOutcome() {}

// ReadTally answers with a tally whose count's type only its own package can name.
//
//nocontract:route GET /tally
func ReadTally(context.Context, nocontract.NoInput) TallyOutcome { return TallyRead{} }
