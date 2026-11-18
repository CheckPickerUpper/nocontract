package routes

import (
	"context"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

// MarkedOutcome holds one member for each way an ErGo marking decides nothing.
//
//sumtype:decl
type MarkedOutcome interface {
	isMarkedOutcome()
}

// Marked is the outcome's success.
//
//ergo:not-an-error
//nocontract:status 200
type Marked struct{}

// Unmarked carries no ErGo marking.
//
//nocontract:status 404
type Unmarked struct{}

// DoublyMarked is marked both a failure with a code and not an error.
//
//ergo:code RF-1006
//ergo:not-an-error
//nocontract:status 404
type DoublyMarked struct{}

// CodedTwice names two codes.
//
//ergo:code RF-1007
//ergo:code RF-1008
//nocontract:status 404
type CodedTwice struct{}

func (Marked) isMarkedOutcome()       {}
func (Unmarked) isMarkedOutcome()     {}
func (DoublyMarked) isMarkedOutcome() {}
func (CodedTwice) isMarkedOutcome()   {}

// ReadMarked answers with the marked outcome.
//
//nocontract:route GET /marked
func ReadMarked(context.Context, nocontract.NoInput) MarkedOutcome { return Marked{} }

// OpenOutcome is not marked //sumtype:decl.
type OpenOutcome interface {
	Open()
}

// ReadOpen answers with an outcome not marked //sumtype:decl.
//
//nocontract:route GET /open
func ReadOpen(context.Context, nocontract.NoInput) OpenOutcome { return Opened{} }

// Opened is the open outcome's only member.
type Opened struct{}

// Open makes Opened an OpenOutcome.
func (Opened) Open() {}

// ExportedOutcome is marked //sumtype:decl, but every method is exported, so it is not sealed.
//
//sumtype:decl
type ExportedOutcome interface {
	IsExportedOutcome()
}

// ReadExported answers with the outcome any package can add a member to.
//
//nocontract:route GET /exported
func ReadExported(context.Context, nocontract.NoInput) ExportedOutcome { return nil }
