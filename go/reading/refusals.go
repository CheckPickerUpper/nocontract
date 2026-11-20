package reading

import (
	ergo "github.com/CheckPickerUpper/ErGo"

	"github.com/CheckPickerUpper/nocontract/messages"
)

// Problem is why a Go declaration is refused: its permanent code and its message, which ErGo
// generates from its declaration.
//
//sumtype:decl
type Problem interface {
	isProblem()
	ErrorCode() ergo.Code
	Report(language messages.Language) ergo.Report
	Causes() []error
}

// RouteDirectiveRepeated is a handler with more than one //nocontract:route line, which serves one
// route.
//
//ergo:code NC-1064
//ergo:message The handler {Handler} has more than one //nocontract:route line; a handler serves one route.
type RouteDirectiveRepeated struct{ Handler string }

// HandlerSignatureUnsupported is a route handler that is not func(context.Context, Input) Outcome.
//
//ergo:code NC-1005
//ergo:message The handler {Handler} is not a func(context.Context, Input) Outcome: {Flaw, select, NotAFunction {it is not a function} TypeParametersDeclared {it has type parameters, so it has no single input or outcome} WrongShape {it does not take a context and an input and return one outcome} ContextMissing {its first parameter is not a context.Context} PointerReceiver {its receiver is a pointer; give the method a value receiver}}.
type HandlerSignatureUnsupported struct {
	Handler string
	Flaw    SignatureFlaw
}

// OutcomeNotNamedInterface is a route whose outcome type is not a named interface.
//
//ergo:code NC-1006
//ergo:message The route's outcome {Type} is not a named interface.
type OutcomeNotNamedInterface struct{ Type string }

// OutcomeNotSealed is an outcome interface marked //sumtype:decl whose every method is exported, so
// any package can add a member, and the methods it has.
//
//ergo:code NC-1007
//ergo:message The outcome {Outcome} is not sealed: its methods {Methods} are all exported; give it an unexported method.
type OutcomeNotSealed struct {
	Outcome string
	Methods string
}

// OutcomeNotDeclared is an outcome interface not marked //sumtype:decl.
//
//ergo:code NC-1105
//ergo:message The outcome {Outcome} is not marked //sumtype:decl.
type OutcomeNotDeclared struct{ Outcome string }

// EndpointWithoutSuccess is a route whose outcome has no member answered with a success status.
//
//ergo:code NC-1028
//ergo:message The route {Endpoint} has no outcome member answered with a success status.
type EndpointWithoutSuccess struct{ Endpoint string }

// DeclarationOutsideProject is a type declared in a package the project file does not list.
//
//ergo:code NC-1008
//ergo:message {Type} is declared in a package the project file does not list.
type DeclarationOutsideProject struct{ Type string }

// PointerMember is a type whose pointer, not the type itself, implements an outcome.
//
//ergo:code NC-1009
//ergo:message Only *{Member} implements the outcome; give its methods value receivers.
type PointerMember struct{ Member string }

// MemberNotStruct is an outcome member that is not a struct.
//
//ergo:code NC-1010
//ergo:message The outcome member {Member} is not a struct.
type MemberNotStruct struct{ Member string }

// GenericMember is an outcome member or named struct with type parameters.
//
//ergo:code NC-1011
//ergo:message {Member} has type parameters, so it has no single name on the wire.
type GenericMember struct{ Member string }

// FailureWithoutCode is a member answered with a failure status but no //ergo:code.
//
//ergo:code NC-1012
//ergo:message {Member} answers {Status}, a failure, but carries no //ergo:code.
type FailureWithoutCode struct {
	Member string
	Status int
}

// SuccessWithCode is a member answered with a success status but marked //ergo:code.
//
//ergo:code NC-1013
//ergo:message {Member} answers {Status}, a success, but is marked //ergo:code; mark it //ergo:not-an-error.
type SuccessWithCode struct {
	Member string
	Status int
}

// MemberUndecided is a member marked neither //ergo:code nor //ergo:not-an-error.
//
//ergo:code NC-1014
//ergo:message {Member} is marked neither //ergo:code nor //ergo:not-an-error; mark it with one.
type MemberUndecided struct{ Member string }

// MemberMarkedBoth is a member marked both //ergo:code and //ergo:not-an-error, and the codes it
// names.
//
//ergo:code NC-1106
//ergo:message {Member} is marked both //ergo:code {Codes} and //ergo:not-an-error; keep one.
type MemberMarkedBoth struct {
	Member string
	Codes  string
}

// CodeRepeated is a member marked //ergo:code more than once, and each code it names.
//
//ergo:code NC-1107
//ergo:message {Member} is marked //ergo:code more than once, with {Codes}; a failure has one code.
type CodeRepeated struct {
	Member string
	Codes  string
}

// FieldOptionUnsupported is a JSON field whose tag carries an option, such as omitempty.
//
//ergo:code NC-1015
//ergo:message The field {Field} carries the JSON option "{Option}"; a value that may be absent is a mo.Option.
type FieldOptionUnsupported struct {
	Field  string
	Option string
}

// EmbeddedFieldUnsupported is an embedded field, which has no single JSON name.
//
//ergo:code NC-1016
//ergo:message The embedded field {Field} has no single JSON name; give it a named field.
type EmbeddedFieldUnsupported struct{ Field string }

// TypeUnsupported is a field type with no JSON shape the Effect API can declare.
//
//ergo:code NC-1017
//ergo:message The type {Type} cannot be declared on the wire: {Flaw, select, ByteSliceField {encoding/json writes a byte slice as base64 text} MapKeyNotText {only a map with string keys is a JSON object} PointerField {a pointer may be nil; a value that may be absent is a mo.Option} InterfaceField {an interface has no single JSON shape} ShapelessField {encoding/json cannot write it}}.
type TypeUnsupported struct {
	Type string
	Flaw TypeFlaw
}

// RecursiveStruct is a recursive wire type whose cycle contains no named record.
//
//ergo:code NC-1019
//ergo:message The recursive type {Struct} must pass through a named record.
type RecursiveStruct struct{ Struct string }

// InputNotStruct is a route input that is not a named struct.
//
//ergo:code NC-1020
//ergo:message The route input {Input} is not a named struct.
type InputNotStruct struct{ Input string }

// InputFieldUnread is an input field with no path or query tag that is not named Body.
//
//ergo:code NC-1021
//ergo:message The input field {Field} has no path or query tag and is not named Body.
type InputFieldUnread struct{ Field string }

// FieldPlacedTwice is an input field tagged both path and query, which names two parts of the
// request where a field is read from one.
//
//ergo:code NC-1112
//ergo:message The input field {Field} is tagged both path:"{Path}" and query:"{Query}", but a field is read from one part of the request.
type FieldPlacedTwice struct {
	Field string
	Path  string
	Query string
}

// TypeNotReachable is an unexported type declared in a package other than the route's, which the
// route's generated code cannot name.
//
//ergo:code NC-1027
//ergo:message {Type} is unexported and declared in another package, so the route's generated code cannot name it.
type TypeNotReachable struct{ Type string }

// StatusDirectiveMissing is an outcome member without a //nocontract:status line.
//
//ergo:code NC-1029
//ergo:message The outcome member {Member} has no //nocontract:status line.
type StatusDirectiveMissing struct{ Member string }

// ParameterTypeUnsupported is a path or query field whose type is not text, a whole number, a
// boolean or a set of string constants.
//
//ergo:code NC-1030
//ergo:message The path or query field {Field} is a {Type}, not text, a whole number, a boolean or a set of string constants.
type ParameterTypeUnsupported struct {
	Field string
	Type  string
}

// GuardUndeclared is a route naming a guard no sealed refusal of the project declares.
//
//ergo:code NC-1046
//ergo:message No sealed refusal marked //nocontract:guard {Guard} is declared in the project's packages.
type GuardUndeclared struct{ Guard string }

// GuardRepeated is a guard name two refusal types both declare, and where the first declares it.
//
//ergo:code NC-1047
//ergo:message The guard {Guard} is already declared at {First}.
type GuardRepeated struct {
	Guard string
	First string
}

// GuardMemberSucceeds is a member of a guard's refusal answered with a success status.
//
//ergo:code NC-1048
//ergo:message {Member} answers {Status}, a success, but a guard's refusal holds only failures.
type GuardMemberSucceeds struct {
	Member string
	Status int
}

// ReceiverUnsupported is a method handler whose receiver type the routes cannot be served with.
//
//ergo:code NC-1049
//ergo:message The routes cannot be served with the receiver {Receiver}: {Flaw, select, ReceiverElsewhere {its type is declared in another package} ReceiverUnexported {its type is unexported, so code mounting the routes cannot name it}}.
type ReceiverUnsupported struct {
	Receiver string
	Flaw     ReceiverFlaw
}

// GeneratedNameTaken is a name the package's generated file declares, already declared by the
// package or by another field of what its routes are served with.
//
//ergo:code NC-1051
//ergo:message The generated file declares {Name}, which the package already uses.
type GeneratedNameTaken struct{ Name string }

// DirectiveUnknown is a //nocontract: line naming no directive NoContract reads, such as a
// misspelled one, which would otherwise be ignored.
//
//ergo:code NC-1056
//ergo:message "{Text}" names no directive NoContract reads: route, status or guard.
type DirectiveUnknown struct{ Text string }

func (RouteDirectiveRepeated) isProblem()      {}
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
func (MemberMarkedBoth) isProblem()            {}
func (CodeRepeated) isProblem()                {}
func (OutcomeNotDeclared) isProblem()          {}
func (EndpointWithoutSuccess) isProblem()      {}
func (FieldOptionUnsupported) isProblem()      {}
func (EmbeddedFieldUnsupported) isProblem()    {}
func (TypeUnsupported) isProblem()             {}
func (RecursiveStruct) isProblem()             {}
func (InputNotStruct) isProblem()              {}
func (InputFieldUnread) isProblem()            {}
func (FieldPlacedTwice) isProblem()            {}
func (TypeNotReachable) isProblem()            {}
func (StatusDirectiveMissing) isProblem()      {}
func (ParameterTypeUnsupported) isProblem()    {}
func (GuardUndeclared) isProblem()             {}
func (GuardRepeated) isProblem()               {}
func (GuardMemberSucceeds) isProblem()         {}
func (ReceiverUnsupported) isProblem()         {}
func (GeneratedNameTaken) isProblem()          {}
func (DirectiveUnknown) isProblem()            {}
