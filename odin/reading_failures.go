package odin

import (
	"fmt"
	"strings"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/CheckPickerUpper/ErGo/codes"
	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

// DeclarationCause retains a parser's failure and the native declaration it belongs to.
type DeclarationCause struct {
	Position    assembling.Position
	Declaration string
	Cause       error
}

// Error describes the declaration and its original parser failure.
func (failure DeclarationCause) Error() string {
	return fmt.Sprintf("%s Odin declaration %s: %v", failure.Position.String(), failure.Declaration, failure.Cause)
}

// Unwrap preserves the parser's failure.
func (failure DeclarationCause) Unwrap() error { return failure.Cause }

// IntegerWidthUnsupported retains the compiler width that has no wire integer.
type IntegerWidthUnsupported struct {
	Position assembling.Position
	Bytes    int
	Signed   bool
}

// Error describes the exact integer width the reader refused.
func (failure IntegerWidthUnsupported) Error() string {
	return fmt.Sprintf("%s unsupported Odin integer width %d bytes, signed %t", failure.Position.String(), failure.Bytes, failure.Signed)
}

// FloatWidthUnsupported retains a compiler floating-point width outside f32 and f64.
type FloatWidthUnsupported struct {
	Position assembling.Position
	Bytes    int
}

// Error describes the exact floating-point width the reader refused.
func (failure FloatWidthUnsupported) Error() string {
	return fmt.Sprintf("%s unsupported Odin floating-point width %d bytes", failure.Position.String(), failure.Bytes)
}

// CodePrefixInvalid retains the source and the shared parser's refused code prefix.
type CodePrefixInvalid struct {
	Position assembling.Position
	Member   string
	Written  string
	Problem  codes.InvalidPrefix
}

// Error describes the code prefix and the exact rule it broke.
func (failure CodePrefixInvalid) Error() string {
	return fmt.Sprintf("%s Odin member %s has invalid code %q: %#v", failure.Position.String(), failure.Member, failure.Written, failure.Problem)
}

// CodeInvalid retains the source and the shared parser's refused permanent code.
type CodeInvalid struct {
	Position assembling.Position
	Member   string
	Problem  codes.InvalidCode
}

// Error describes the permanent code and the exact rule it broke.
func (failure CodeInvalid) Error() string {
	return fmt.Sprintf("%s Odin member %s has invalid code: %#v", failure.Position.String(), failure.Member, failure.Problem)
}

// MemberStatusContradiction retains the HTTP status and the ErOdin attributes it contradicts.
type MemberStatusContradiction struct {
	Position   assembling.Position
	Member     string
	Status     description.Status
	Attributes []compiler.Attribute
}

// Error describes the two classifications that disagree.
func (failure MemberStatusContradiction) Error() string {
	return fmt.Sprintf("%s Odin member %s status %d contradicts attributes %v", failure.Position.String(), failure.Member, failure.Status.Code(), failure.Attributes)
}

type portableSourceSiteUnavailable struct {
	Project string
	Source  assembling.Position
	Cause   error
}

func (failure portableSourceSiteUnavailable) Error() string {
	return fmt.Sprintf("describing %s relative to %s: %v", failure.Source, failure.Project, failure.Cause)
}

func (failure portableSourceSiteUnavailable) Unwrap() error { return failure.Cause }

func parseFailureCode(code string, declaration compiler.Declaration) outcome.Result[ergo.Code] {
	prefix, _, _ := strings.Cut(code, "-")
	switch parsed := codes.ParsePrefix(prefix).(type) {
	case codes.ParsedPrefix:
		switch parsed := codes.Parse(codes.ParseRequest{Text: code, Prefix: parsed.Prefix}).(type) {
		case codes.ParsedCode:
			return outcome.Ok(parsed.Code)
		case codes.InvalidCode:
			return outcome.Err[ergo.Code](CodeInvalid{Position: placed(declaration), Member: declaration.Entity.Name, Problem: parsed})
		case nil:
		}
	case codes.InvalidPrefix:
		return outcome.Err[ergo.Code](CodePrefixInvalid{Position: placed(declaration), Member: declaration.Entity.Name, Written: code, Problem: parsed})
	case nil:
	}
	return outcome.Err[ergo.Code](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: "error code has no parsed outcome"})
}
