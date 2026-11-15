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

// Embedding embeds a struct.
//
//ergo:not-an-error
//nocontract:status 200
type Embedding struct {
	Read
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
