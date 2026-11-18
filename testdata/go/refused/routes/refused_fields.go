package routes

import (
	"context"
)

// MisnamedOutcome holds a member for each way a field's name is refused.
//
//sumtype:decl
type MisnamedOutcome interface {
	isMisnamedOutcome()
}

// Misnamed is a success whose fields are named wrong.
//
//ergo:not-an-error
//nocontract:status 200
type Misnamed struct {
	Unquoted  string `json:item`
	Quoted    string `json:"it's"`
	First     string `json:"twin"`
	Second    string `json:"twin"`
	Untouched string `json:"untouched"`
}

func (Misnamed) isMisnamedOutcome() {}

// MisnamedInput reads parameters whose names are refused.
type MisnamedInput struct {
	Blank    string `path:""`
	Nameless string `query:""`
	Sort     string `query:"sort"`
	Order    string `query:"sort"`
	Bare     string `query:page`
}

// ReadMisnamed answers with fields and reads parameters named wrong.
//
//nocontract:route GET /misnamed
func ReadMisnamed(context.Context, MisnamedInput) MisnamedOutcome { return Misnamed{} }
