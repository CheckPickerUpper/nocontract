package odin

import (
	"fmt"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
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
