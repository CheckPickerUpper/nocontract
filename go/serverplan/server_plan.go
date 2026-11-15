// Package serverplan is what the Go writer needs, beyond the language-neutral description, to
// write the code of each package: the Go names of each route's handler, guard, input and outcome,
// how each input field is read, and each member's Go type. It owns the names the generated file
// declares, which the reader keeps free and the command expects before the file is first written.
package serverplan

import (
	"path/filepath"

	"github.com/CheckPickerUpper/nocontract/description"
)

// FileName is the Go file written into each package that declares a route or a guard's refusal.
const FileName = "nocontract_routes.go"

// NoContractPath is the import path of the Go package the generated code is built from, whose own
// failures every route can answer.
const NoContractPath = "github.com/CheckPickerUpper/nocontract/go"

// SourceFile is who wrote a Go file: NoContract, or the project.
//
//sumtype:decl
type SourceFile interface{ isSourceFile() }

// GeneratedFile is a file NoContract writes and rewrites, which no declaration is read from.
//
//ergo:not-an-error
type GeneratedFile struct{}

// WrittenFile is a file the project wrote.
//
//ergo:not-an-error
type WrittenFile struct{}

func (GeneratedFile) isSourceFile() {}
func (WrittenFile) isSourceFile()   {}

// SourceFileAt is who wrote the Go file at path.
func SourceFileAt(path string) SourceFile {
	if filepath.Base(path) == FileName {
		return GeneratedFile{}
	}
	return WrittenFile{}
}

// The names a generated file declares that code outside it calls.
const (
	RoutesFunction      = "Routes"
	ServingType         = "Serving"
	RefusalAnswerPrefix = "NocontractAnswer"
)

// CalledNames are the names a package's generated file declares beside its routes, which the
// package may not declare itself.
func CalledNames() []string {
	return []string{RoutesFunction, ServingType}
}

// Plan is the generated code of every Go package that declares a route or a guard's refusal.
type Plan struct {
	Packages []Package
}

// Package is one Go package's generated code: its name, its import path, the folder its generated
// file is written to, the routes it serves, and the guard refusals it declares.
type Package struct {
	Name      string
	Path      string
	Directory string
	Serves    Routing
	Refusals  []GuardRefusal
}

// Routing is whether a package serves routes.
//
//sumtype:decl
type Routing interface{ isRouting() }

// ServesNoRoutes is a package that declares only guard refusals.
//
//ergo:not-an-error
type ServesNoRoutes struct{}

// ServesRoutes is a package's routes, what they are served with, and each outcome they answer,
// once.
//
//ergo:not-an-error
type ServesRoutes struct {
	Routes   []Route
	Serving  Serving
	Outcomes []OutcomeAnswer
}

func (ServesNoRoutes) isRouting() {}
func (ServesRoutes) isRouting()   {}

// OutcomeAnswer is one outcome a package's routes answer and each of its members.
type OutcomeAnswer struct {
	Outcome GoName
	Members []Member
}

// Serving is what a package's routes are served with: the receiver of each method handler, by its
// type, and each guard its routes name, in order.
type Serving struct {
	Receivers []GoName
	Guards    []string
}

// GuardRefusal is a guard's sealed refusal declared in the package: the guard's name, the refusal
// type and each of its members.
type GuardRefusal struct {
	Guard   string
	Outcome GoName
	Members []Member
}

// GoName is a Go type: its package's import path, empty for a predeclared type, and its name.
type GoName struct {
	Package string
	Name    string
}

// Handler is what answers a route: a function, or a method of a receiver the routes are served
// with.
//
//sumtype:decl
type Handler interface{ isHandler() }

// FunctionHandler is a function of the route's package.
//
//ergo:not-an-error
type FunctionHandler struct{ Name string }

// MethodHandler is a method of a receiver type of the route's package.
//
//ergo:not-an-error
type MethodHandler struct {
	Receiver string
	Method   string
}

func (FunctionHandler) isHandler() {}
func (MethodHandler) isHandler()   {}

// RouteGuard is the guard a route is served behind, if any.
//
//sumtype:decl
type RouteGuard interface{ isRouteGuard() }

// Unguarded is a route served behind no guard.
//
//ergo:not-an-error
type Unguarded struct{}

// Guarded is a route served behind the guard Name, which is handed Argument.
//
//ergo:not-an-error
type Guarded struct {
	Name     string
	Argument string
}

func (Unguarded) isRouteGuard() {}
func (Guarded) isRouteGuard()   {}

// Route is one route as the generated code serves it: its endpoint name, method, path, handler,
// guard, input and outcome, and how its input is read.
type Route struct {
	Name    string
	Method  description.Method
	Path    string
	Handler Handler
	Guard   RouteGuard
	Input   GoName
	Outcome GoName
	Params  []Param
	Query   []Param
	Body    BodyRead
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

// Member is one member of an outcome or a guard's refusal: its Go type, whose name is its tag, and
// how it answers.
type Member struct {
	Go     GoName
	Answer description.Answer
}
