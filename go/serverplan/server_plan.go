// Package serverplan is what the Go writer needs, beyond the language-neutral description, to
// write the code that serves a package's routes: the Go names of each route's handler, input and
// outcome, how each input field is read, and each member's Go type.
package serverplan

// Plan is the routes of every Go package that declares one.
type Plan struct {
	Packages []Package
}

// Package is one Go package's routes: its name, its import path and the folder its generated file
// is written to.
type Package struct {
	Name      string
	Path      string
	Directory string
	Routes    []Route
}

// GoName is a Go type or function: its package's import path, empty for a predeclared type, and
// its name.
type GoName struct {
	Package string
	Name    string
}

// Route is one route as the generated code serves it.
type Route struct {
	Method  string
	Path    string
	Handler GoName
	Input   GoName
	Outcome GoName
	Params  []Param
	Query   []Param
	Body    BodyRead
	Members []Member
}

// Param is one input field read from a path segment or a query parameter: its name in the
// request, its Go field, how its text is read and the Go type the read value converts to.
type Param struct {
	Name    string
	GoField string
	Text    TextKind
	GoType  GoName
}

// TextKind is how a parameter's text is read.
//
//sumtype:decl
type TextKind interface{ isTextKind() }

// TextAsIs is text read as it is.
//
//ergo:not-an-error
type TextAsIs struct{}

// TextSigned is text read as a whole number that fits Bits bits, 0 meaning an int's size.
//
//ergo:not-an-error
type TextSigned struct{ Bits int }

// TextUnsigned is text read as a whole number at or above zero that fits Bits bits.
//
//ergo:not-an-error
type TextUnsigned struct{ Bits int }

// TextTruth is text read as a boolean.
//
//ergo:not-an-error
type TextTruth struct{}

// TextLiteral is text that must be one of Values.
//
//ergo:not-an-error
type TextLiteral struct{ Values []string }

func (TextAsIs) isTextKind()     {}
func (TextSigned) isTextKind()   {}
func (TextUnsigned) isTextKind() {}
func (TextTruth) isTextKind()    {}
func (TextLiteral) isTextKind()  {}

// BodyRead is whether a route decodes its input's Body field from the JSON body.
//
//sumtype:decl
type BodyRead interface{ isBodyRead() }

// NoBodyRead is a route that reads no body.
//
//ergo:not-an-error
type NoBodyRead struct{}

// BodyFieldRead is a route that decodes the JSON body into its input's Body field.
//
//ergo:not-an-error
type BodyFieldRead struct{}

func (NoBodyRead) isBodyRead()    {}
func (BodyFieldRead) isBodyRead() {}

// Member is one member of a route's outcome: its Go type, its tag, its status and a failure's
// code.
type Member struct {
	Go     GoName
	Tag    string
	Status int
	Code   MemberCode
}

// MemberCode is whether a member answers with a permanent code.
//
//sumtype:decl
type MemberCode interface{ isMemberCode() }

// NoCode is a success, which carries no code.
//
//ergo:not-an-error
type NoCode struct{}

// Coded is a failure and its permanent code.
//
//ergo:not-an-error
type Coded struct{ Code string }

func (NoCode) isMemberCode() {}
func (Coded) isMemberCode()  {}
