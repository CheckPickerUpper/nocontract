package reading

import (
	"fmt"
	"go/token"
)

// Refusal is one declaration the generator cannot turn into served routes and their Effect API,
// where it is written and why.
type Refusal struct {
	Position token.Position
	Problem  Problem
}

// Problem is why a declaration is refused.
//
//sumtype:decl
type Problem interface {
	isProblem()
	Explain() string
}

// RouteDirectiveMalformed is a //nocontract:route line that is not a method and a path.
//
//ergo:code NC-1004
type RouteDirectiveMalformed struct{ Text string }

// HandlerSignatureUnsupported is a route handler that is not func(context.Context, Input) Outcome.
//
//ergo:code NC-1005
type HandlerSignatureUnsupported struct {
	Handler string
	Reason  string
}

// OutcomeNotNamedInterface is a route whose outcome type is not a named interface.
//
//ergo:code NC-1006
type OutcomeNotNamedInterface struct{ Type string }

// OutcomeNotSealed is an outcome interface without //sumtype:decl and an unexported method.
//
//ergo:code NC-1007
type OutcomeNotSealed struct{ Outcome string }

// DeclarationOutsideProject is a type declared in a package the project file does not list.
//
//ergo:code NC-1008
type DeclarationOutsideProject struct{ Type string }

// PointerMember is a type whose pointer, not the type itself, implements an outcome.
//
//ergo:code NC-1009
type PointerMember struct{ Member string }

// MemberNotStruct is an outcome member that is not a struct.
//
//ergo:code NC-1010
type MemberNotStruct struct{ Member string }

// GenericMember is an outcome member or named struct with type parameters.
//
//ergo:code NC-1011
type GenericMember struct{ Member string }

// FailureWithoutCode is a member answered with a failure status but no //ergo:code.
//
//ergo:code NC-1012
type FailureWithoutCode struct {
	Member string
	Status int
}

// SuccessWithCode is a member answered with a success status but marked //ergo:code.
//
//ergo:code NC-1013
type SuccessWithCode struct {
	Member string
	Status int
}

// MemberUndecided is a member without exactly one ErGo decision.
//
//ergo:code NC-1014
type MemberUndecided struct{ Member string }

// FieldOptionUnsupported is a JSON field whose tag carries an option, such as omitempty.
//
//ergo:code NC-1015
type FieldOptionUnsupported struct {
	Field  string
	Option string
}

// EmbeddedFieldUnsupported is an embedded field, which has no single JSON name.
//
//ergo:code NC-1016
type EmbeddedFieldUnsupported struct{ Field string }

// TypeUnsupported is a field type with no JSON shape the Effect API can declare.
//
//ergo:code NC-1017
type TypeUnsupported struct {
	Type   string
	Reason string
}

// NameRepeated is a name two declarations would both take in the TypeScript file.
//
//ergo:code NC-1018
type NameRepeated struct {
	Name  string
	First token.Position
}

// RecursiveStruct is a named struct that holds itself.
//
//ergo:code NC-1019
type RecursiveStruct struct{ Struct string }

// InputNotStruct is a route input that is not a named struct.
//
//ergo:code NC-1020
type InputNotStruct struct{ Input string }

// InputFieldUnread is an input field with no path or query tag that is not named Body.
//
//ergo:code NC-1021
type InputFieldUnread struct{ Field string }

// PathParameterUndeclared is a path segment no input field reads.
//
//ergo:code NC-1022
type PathParameterUndeclared struct{ Parameter string }

// PathFieldUnused is an input field reading a path segment the path does not have.
//
//ergo:code NC-1023
type PathFieldUnused struct{ Parameter string }

// PathWildcardUnsupported is a path holding a {name...} or {$} segment.
//
//ergo:code NC-1024
type PathWildcardUnsupported struct{ Segment string }

// BodyOnGet is a GET route whose input reads a body.
//
//ergo:code NC-1025
type BodyOnGet struct{}

// RouteRepeated is a method and path two routes both serve.
//
//ergo:code NC-1026
type RouteRepeated struct {
	Pattern string
	First   token.Position
}

// TypeNotReachable is an unexported type declared in a package other than the route's, which the
// route's generated code cannot name.
//
//ergo:code NC-1027
type TypeNotReachable struct{ Type string }

// OutcomeWithoutSuccess is an outcome with no member answered with a success status.
//
//ergo:code NC-1028
type OutcomeWithoutSuccess struct{ Outcome string }

// StatusDirectiveMissing is an outcome member without a //nocontract:status line.
//
//ergo:code NC-1029
type StatusDirectiveMissing struct{ Member string }

// ParameterTypeUnsupported is a path or query field whose type is not text, a whole number, a
// boolean or a set of string constants.
//
//ergo:code NC-1030
type ParameterTypeUnsupported struct {
	Field string
	Type  string
}

// StatusDirectiveMalformed is a //nocontract:status line that is not one known HTTP status, or a
// member with more than one.
//
//ergo:code NC-1032
type StatusDirectiveMalformed struct {
	Member string
	Text   string
}

// FieldNameReserved is a member field whose JSON name is one every answer starts with.
//
//ergo:code NC-1033
type FieldNameReserved struct{ Field string }

func (RouteDirectiveMalformed) isProblem()     {}
func (HandlerSignatureUnsupported) isProblem() {}
func (OutcomeNotNamedInterface) isProblem()    {}
func (OutcomeNotSealed) isProblem()            {}
func (DeclarationOutsideProject) isProblem()   {}
func (PointerMember) isProblem()               {}
func (MemberNotStruct) isProblem()             {}
func (GenericMember) isProblem()               {}
func (FailureWithoutCode) isProblem()          {}
func (SuccessWithCode) isProblem()             {}
func (MemberUndecided) isProblem()             {}
func (FieldOptionUnsupported) isProblem()      {}
func (EmbeddedFieldUnsupported) isProblem()    {}
func (TypeUnsupported) isProblem()             {}
func (NameRepeated) isProblem()                {}
func (RecursiveStruct) isProblem()             {}
func (InputNotStruct) isProblem()              {}
func (InputFieldUnread) isProblem()            {}
func (PathParameterUndeclared) isProblem()     {}
func (PathFieldUnused) isProblem()             {}
func (PathWildcardUnsupported) isProblem()     {}
func (BodyOnGet) isProblem()                   {}
func (RouteRepeated) isProblem()               {}
func (TypeNotReachable) isProblem()            {}
func (OutcomeWithoutSuccess) isProblem()       {}
func (StatusDirectiveMissing) isProblem()      {}
func (ParameterTypeUnsupported) isProblem()    {}
func (StatusDirectiveMalformed) isProblem()    {}
func (FieldNameReserved) isProblem()           {}

// Explain says why the directive is refused.
func (refused RouteDirectiveMalformed) Explain() string {
	return fmt.Sprintf("the route directive %q is not a method (GET, POST, PUT, PATCH or DELETE) and a path starting with /", refused.Text)
}

// Explain says why the handler is refused.
func (refused HandlerSignatureUnsupported) Explain() string {
	return fmt.Sprintf("the handler %s is not a func(context.Context, Input) Outcome: %s", refused.Handler, refused.Reason)
}

// Explain says why the outcome is refused.
func (refused OutcomeNotNamedInterface) Explain() string {
	return fmt.Sprintf("the route's outcome %s is not a named interface", refused.Type)
}

// Explain says why the outcome is refused.
func (refused OutcomeNotSealed) Explain() string {
	return fmt.Sprintf("the outcome %s is not sealed: mark it //sumtype:decl and give it an unexported method", refused.Outcome)
}

// Explain says why the type is refused.
func (refused DeclarationOutsideProject) Explain() string {
	return fmt.Sprintf("%s is declared in a package the project file does not list", refused.Type)
}

// Explain says why the member is refused.
func (refused PointerMember) Explain() string {
	return fmt.Sprintf("only *%s implements the outcome; give the methods value receivers", refused.Member)
}

// Explain says why the member is refused.
func (refused MemberNotStruct) Explain() string {
	return fmt.Sprintf("the outcome member %s is not a struct", refused.Member)
}

// Explain says why the type is refused.
func (refused GenericMember) Explain() string {
	return fmt.Sprintf("%s has type parameters, so it has no single name on the wire", refused.Member)
}

// Explain says why the member is refused.
func (refused FailureWithoutCode) Explain() string {
	return fmt.Sprintf("%s answers %d, a failure, but carries no //ergo:code", refused.Member, refused.Status)
}

// Explain says why the member is refused.
func (refused SuccessWithCode) Explain() string {
	return fmt.Sprintf("%s answers %d, a success, but is marked //ergo:code; mark it //ergo:not-an-error", refused.Member, refused.Status)
}

// Explain says why the member is refused.
func (refused MemberUndecided) Explain() string {
	return fmt.Sprintf("%s needs exactly one of //ergo:code and //ergo:not-an-error", refused.Member)
}

// Explain says why the field is refused.
func (refused FieldOptionUnsupported) Explain() string {
	return fmt.Sprintf("the field %s carries the JSON option %q; a value that may be absent is a mo.Option", refused.Field, refused.Option)
}

// Explain says why the field is refused.
func (refused EmbeddedFieldUnsupported) Explain() string {
	return fmt.Sprintf("the embedded field %s has no single JSON name; give it a named field", refused.Field)
}

// Explain says why the type is refused.
func (refused TypeUnsupported) Explain() string {
	return fmt.Sprintf("the type %s cannot be declared on the wire: %s", refused.Type, refused.Reason)
}

// Explain says why the name is refused.
func (refused NameRepeated) Explain() string {
	return fmt.Sprintf("the name %s is already taken by the declaration at %s", refused.Name, refused.First)
}

// Explain says why the struct is refused.
func (refused RecursiveStruct) Explain() string {
	return fmt.Sprintf("the struct %s holds itself", refused.Struct)
}

// Explain says why the input is refused.
func (refused InputNotStruct) Explain() string {
	return fmt.Sprintf("the route input %s is not a named struct", refused.Input)
}

// Explain says why the field is refused.
func (refused InputFieldUnread) Explain() string {
	return fmt.Sprintf("the input field %s has no path or query tag and is not named Body", refused.Field)
}

// Explain says why the path is refused.
func (refused PathParameterUndeclared) Explain() string {
	return fmt.Sprintf("no input field reads the path segment {%s}", refused.Parameter)
}

// Explain says why the field is refused.
func (refused PathFieldUnused) Explain() string {
	return fmt.Sprintf("an input field reads the path segment {%s}, which the path does not have", refused.Parameter)
}

// Explain says why the path is refused.
func (refused PathWildcardUnsupported) Explain() string {
	return fmt.Sprintf("the path segment %s has no Effect HttpApi form", refused.Segment)
}

// Explain says why the input is refused.
func (BodyOnGet) Explain() string {
	return "a GET route's input reads a body, which a GET request does not carry"
}

// Explain says why the route is refused.
func (refused RouteRepeated) Explain() string {
	return fmt.Sprintf("%s is already served by the route at %s", refused.Pattern, refused.First)
}

// Explain says why the type is refused.
func (refused TypeNotReachable) Explain() string {
	return fmt.Sprintf("%s is unexported and declared in another package, so the route's generated code cannot name it", refused.Type)
}

// Explain says why the outcome is refused.
func (refused OutcomeWithoutSuccess) Explain() string {
	return fmt.Sprintf("the outcome %s has no member answered with a success status", refused.Outcome)
}

// Explain says why the member is refused.
func (refused StatusDirectiveMissing) Explain() string {
	return fmt.Sprintf("the outcome member %s has no //nocontract:status line", refused.Member)
}

// Explain says why the field is refused.
func (refused ParameterTypeUnsupported) Explain() string {
	return fmt.Sprintf("the path or query field %s is a %s, not text, a whole number, a boolean or a set of string constants", refused.Field, refused.Type)
}

// Explain says why the member is refused.
func (refused StatusDirectiveMalformed) Explain() string {
	return fmt.Sprintf("%s's status %q is not one known HTTP status, written once", refused.Member, refused.Text)
}

// Explain says why the field is refused.
func (refused FieldNameReserved) Explain() string {
	return fmt.Sprintf("the JSON name %q is taken by the tag and code every answer starts with", refused.Field)
}
