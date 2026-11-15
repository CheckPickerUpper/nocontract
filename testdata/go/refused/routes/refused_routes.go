// Package routes declares one route for each way a declaration is refused.
package routes

import (
	"context"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

// ReadOutcome is a well-formed outcome the refused routes share.
//
//sumtype:decl
type ReadOutcome interface {
	isReadOutcome()
}

// Read is a well-formed success.
//
//ergo:not-an-error
//nocontract:status 200
type Read struct {
	Text string `json:"text"`
}

func (Read) isReadOutcome() {}

// Malformed names no path.
//
//nocontract:route GET
func Malformed(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Contextless takes no context.
//
//nocontract:route GET /contextless
func Contextless(nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Unread names a path segment its input does not read.
//
//nocontract:route GET /items/{item_id}
func Unread(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Wildcard serves a {path...} segment.
//
//nocontract:route GET /files/{path...}
func Wildcard(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// BodyInput reads a body, which a GET route cannot.
type BodyInput struct {
	Body struct {
		Text string `json:"text"`
	}
}

// ReadBody answers a GET route that reads a body.
//
//nocontract:route GET /body
func ReadBody(context.Context, BodyInput) ReadOutcome { return Read{Text: ""} }

// OpenOutcome is not sealed.
type OpenOutcome interface {
	Open()
}

// ReadOpen answers with an unsealed outcome.
//
//nocontract:route GET /open
func ReadOpen(context.Context, nocontract.NoInput) OpenOutcome { return Opened{} }

// Opened is the open outcome's only member.
type Opened struct{}

// Open makes Opened an OpenOutcome.
func (Opened) Open() {}

// FlawedOutcome holds one member for each way a member is refused.
//
//sumtype:decl
type FlawedOutcome interface {
	isFlawedOutcome()
}

// Fine is the outcome's success.
//
//ergo:not-an-error
//nocontract:status 200
type Fine struct{}

// MissingButUncoded answers 404 without a code.
//
//ergo:not-an-error
//nocontract:status 404
type MissingButUncoded struct{}

// FineButCoded answers 201 with a code.
//
//ergo:code RF-1001
//nocontract:status 201
type FineButCoded struct{}

// Undecided carries no ErGo decision.
//
//nocontract:status 202
type Undecided struct{}

// Optional marks a field omitempty.
//
//ergo:not-an-error
//nocontract:status 200
type Optional struct {
	Note string `json:"note,omitempty"`
}

// Pointed holds a pointer.
//
//ergo:not-an-error
//nocontract:status 200
type Pointed struct {
	Note *string `json:"note"`
}

// Recoded names a field code.
//
//ergo:code RF-1002
//nocontract:status 409
type Recoded struct {
	Code string `json:"code"`
}

// Statusless declares no status.
//
//ergo:code RF-1003
type Statusless struct{}

// Teapot declares a status that carries no body.
//
//ergo:not-an-error
//nocontract:status 204
type Teapot struct{}

// Detail is a struct only Embedding embeds.
type Detail struct {
	Note string `json:"note"`
}

// Embedding embeds a struct.
//
//ergo:not-an-error
//nocontract:status 200
type Embedding struct {
	Detail
}

func (Fine) isFlawedOutcome()              {}
func (MissingButUncoded) isFlawedOutcome() {}
func (FineButCoded) isFlawedOutcome()      {}
func (Undecided) isFlawedOutcome()         {}
func (Optional) isFlawedOutcome()          {}
func (Pointed) isFlawedOutcome()           {}
func (Recoded) isFlawedOutcome()           {}
func (Statusless) isFlawedOutcome()        {}
func (Teapot) isFlawedOutcome()            {}
func (Embedding) isFlawedOutcome()         {}

// ReadFlawed answers with the flawed outcome.
//
//nocontract:route GET /flawed
func ReadFlawed(context.Context, nocontract.NoInput) FlawedOutcome { return Fine{} }

// StaffRefusal is a guard's refusal that holds a success.
//
//sumtype:decl
//nocontract:guard Staff
type StaffRefusal interface {
	isStaffRefusal()
}

// Welcome answers 200 from inside a guard's refusal.
//
//ergo:not-an-error
//nocontract:status 200
type Welcome struct{}

func (Welcome) isStaffRefusal() {}

// UnknownGuard names a guard no refusal declares.
//
//nocontract:route GET /unknown-guard
//nocontract:guard Nobody admin
func UnknownGuard(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// BareGuard names its guard without an argument.
//
//nocontract:route GET /bare-guard
//nocontract:guard Staff
func BareGuard(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// hidden is a receiver type code mounting the routes cannot name.
type hidden struct{}

// Peek is a route answered by a method of an unexported type.
//
//nocontract:route GET /hidden
func (hidden) Peek(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Shelf counts what is on a shelf.
type Shelf struct{}

// Rack counts what is on a rack.
type Rack struct{}

// Count counts a shelf.
//
//nocontract:route GET /shelf
func (Shelf) Count(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Count counts a rack, under the endpoint name the shelf took.
//
//nocontract:route GET /rack
func (Rack) Count(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Misspelled is a handler whose route line names a directive NoContract does not read.
//
//nocontract:rout GET /misspelled
func Misspelled(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Serving takes a name the generated file declares.
type Serving struct{}
