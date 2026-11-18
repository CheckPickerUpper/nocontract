package routes

import (
	"context"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

// Pathless names no path.
//
//nocontract:route GET
func Pathless(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Unrooted names a path that does not start with /.
//
//nocontract:route GET unrooted
func Unrooted(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Fetched names a method no route answers.
//
//nocontract:route FETCH /fetched
func Fetched(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Wordy writes a word after its path.
//
//nocontract:route GET /wordy now
func Wordy(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Unnamed holds a parameter without a name.
//
//nocontract:route GET /unnamed/{}
func Unnamed(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Ended holds the end-of-path wildcard.
//
//nocontract:route GET /ended/{$}
func Ended(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// Rest holds a parameter taking every segment left.
//
//nocontract:route GET /files/{path...}
func Rest(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// BracedName holds a parameter whose name holds a brace.
//
//nocontract:route GET /braced/{a{b}
func BracedName(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// BracedText holds a segment with a brace that does not enclose it whole.
//
//nocontract:route GET /braced/a{b}
func BracedText(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// MisstatedOutcome holds one member for each way a status line is written wrong.
//
//sumtype:decl
type MisstatedOutcome interface {
	isMisstatedOutcome()
}

// Worded writes its status as a word.
//
//ergo:not-an-error
//nocontract:status ok
type Worded struct{}

// Explained writes a word after its status.
//
//ergo:not-an-error
//nocontract:status 200 OK
type Explained struct{}

// Restated writes its status twice.
//
//ergo:not-an-error
//nocontract:status 200
//nocontract:status 201
type Restated struct{}

// Unanswerable answers a status that carries no body.
//
//ergo:not-an-error
//nocontract:status 204
type Unanswerable struct{}

func (Worded) isMisstatedOutcome()       {}
func (Explained) isMisstatedOutcome()    {}
func (Restated) isMisstatedOutcome()     {}
func (Unanswerable) isMisstatedOutcome() {}

// ReadMisstated answers with the misstated outcome.
//
//nocontract:route GET /misstated
func ReadMisstated(context.Context, nocontract.NoInput) MisstatedOutcome { return Worded{} }

// CrowdRefusal is a guard's refusal whose guard line names two guards.
//
//sumtype:decl
//nocontract:guard Crowd Extra
type CrowdRefusal interface {
	isCrowdRefusal()
}

// CrowdTurnedAway is the crowd guard's only member.
//
//ergo:code RF-1005
//nocontract:status 403
type CrowdTurnedAway struct{}

func (CrowdTurnedAway) isCrowdRefusal() {}

// BareGuard names its guard without an argument.
//
//nocontract:route GET /bare-guard
//nocontract:guard Staff
func BareGuard(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// LowerGuard names its guard in a form Go does not export.
//
//nocontract:route GET /lower-guard
//nocontract:guard staff admin
func LowerGuard(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }

// TwiceGuarded writes two guard lines.
//
//nocontract:route GET /twice-guarded
//nocontract:guard Staff admin
//nocontract:guard Staff owner
func TwiceGuarded(context.Context, nocontract.NoInput) ReadOutcome { return Read{Text: ""} }
