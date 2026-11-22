// Package serverplan is what the Go writer needs, beyond the language-neutral description, to
// write the code of each package: the Go names of each route's handler, guard, input and outcome,
// how each input field is read, and each member's Go type. It owns the names the generated file
// declares, which the reader keeps free and the command expects before the file is first written.
package serverplan

import (
	"go/types"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/CheckPickerUpper/nocontract/description"
)

// The Go files NoContract writes into a project's packages.
const (
	// RoutesFileName is the Go file written into each package that declares a route or a guard's
	// refusal.
	RoutesFileName = "nocontract_routes.go"
	// FilesTestFileName is the test written into the package whose folder sorts first among those
	// given a RoutesFileName, which fails while any generated file differs from what generate writes.
	FilesTestFileName = "nocontract_files_test.go"
	// ServerFileName is the Go file written into the project's server folder, which declares the
	// only value that serves the routes.
	ServerFileName = "nocontract_server.go"
)

// Server is the server package NoContract writes: its folder, its name, and whether the project
// wrote Go files of its own there.
type Server struct {
	Directory string
	Name      string
	Home      ServerHome
}

// ServerHome is whether the server folder holds Go files the project wrote.
//
//sumtype:decl
type ServerHome interface{ isServerHome() }

// ServerBesideProjectCode is a server folder holding a package the project wrote: its import path,
// which may declare routes, and every name its files declare, which the server file's imports
// leave free.
//
//ergo:not-an-error
type ServerBesideProjectCode struct {
	Path     string
	Declared []string
}

// ServerAlone is a server folder holding no Go file the project wrote.
//
//ergo:not-an-error
type ServerAlone struct{}

func (ServerBesideProjectCode) isServerHome() {}
func (ServerAlone) isServerHome()             {}

// FilesTest is the test written into one package: the package's name and the project file's path
// from the package's folder, separated by slashes.
type FilesTest struct {
	PackageName string
	ProjectFile string
}

// NoContractPath is the import path of the Go package the generated code is built from, whose own
// failures every route can answer.
const NoContractPath = "github.com/CheckPickerUpper/nocontract/go"

// CheckingPath is the import path of the package whose RequireCurrent the generated test calls.
const CheckingPath = "github.com/CheckPickerUpper/nocontract/checking"

// CheckingPackage is the name of the Go package at CheckingPath, which the generated test refers to
// it by.
const CheckingPackage = "checking"

// NoContractPackage is the name of the Go package at NoContractPath, which every generated file
// refers to it by.
const NoContractPackage = "nocontract"

// The prefixes of the exported names a generated file declares, which code outside it calls.
const (
	// AnswerPrefix begins the name of the function answering each member of a sealed type.
	AnswerPrefix = "NocontractAnswer"
	// RoutePrefix begins the name of the function answering one route, which the server calls.
	RoutePrefix = "NocontractRoute"
)

// RouteFunction is the name of the generated function answering the route of an endpoint name.
func RouteFunction(endpoint string) string {
	first, size := utf8.DecodeRuneInString(endpoint)
	return RoutePrefix + string(unicode.ToUpper(first)) + endpoint[size:]
}

// The names the server file declares in its package.
const (
	// ServerType is the type of the only value that serves the routes.
	ServerType = "API"
	// ServerFunction builds the ServerType from what the routes are served with.
	ServerFunction = "New"
	// ServerListenFunction starts a generated API on the runtime's TCP listener.
	ServerListenFunction = "Listen"
)

// ServerNames are the names the server file declares in its package, which the project's own
// files there may not declare.
func ServerNames() []string {
	return []string{ServerType, ServerFunction, ServerListenFunction}
}

// Plan is the generated code of every Go package that declares a route or a guard's refusal, and
// the server package that serves the routes.
type Plan struct {
	Packages []Package
	Server   Server
}

// Package is one Go package's generated code: its name, its import path, the folder its generated
// file is written to, the routes it serves, and the sealed types it declares whose members code
// outside the generated file answers.
type Package struct {
	Name      string
	Path      string
	Directory string
	Serves    Routing
	Answers   []SealedAnswer
}

// Routing is whether a package serves routes.
//
//sumtype:decl
type Routing interface{ isRouting() }

// ServesNoRoutes is a package that declares only sealed answers.
//
//ergo:not-an-error
type ServesNoRoutes struct{}

// ServesRoutes is a package's routes, what they are served with in the order its routes first
// need it, and each outcome they answer, once.
//
//ergo:not-an-error
type ServesRoutes struct {
	Routes   []Route
	Needs    []RouteNeed
	Outcomes []OutcomeAnswer
}

func (ServesNoRoutes) isRouting() {}
func (ServesRoutes) isRouting()   {}

// OutcomeAnswer is one outcome a package's routes answer and each of its members.
type OutcomeAnswer struct {
	Outcome GoName
	Members []Member
}

// RouteNeed is one thing a package's routes are served with, which the server is handed.
//
//sumtype:decl
type RouteNeed interface{ isRouteNeed() }

// NeedsReceiver is the receiver of the package's method handlers of one type.
//
//ergo:not-an-error
type NeedsReceiver struct{ Type GoName }

// NeedsGuard is a guard the package's routes name.
//
//ergo:not-an-error
type NeedsGuard struct{ Guard Guarding }

func (NeedsReceiver) isRouteNeed() {}
func (NeedsGuard) isRouteNeed()    {}

// Guarding is a guard by its name and the sealed refusal it declares, which types what it decides.
type Guarding struct {
	Name    string
	Refusal GoName
}

// SealedAnswer is a sealed type the package declares whose members code outside the generated
// file answers through the function generated for it: who answers it, the type and each member.
type SealedAnswer struct {
	Use     AnswerUse
	Outcome GoName
	Members []Member
}

// AnswerUse is who answers a sealed type's members through its generated function.
//
//sumtype:decl
type AnswerUse interface{ isAnswerUse() }

// GuardAnswers is a guard's refusal, which the guard of that name answers.
//
//ergo:not-an-error
type GuardAnswers struct{ Guard string }

// RuntimeAnswers is nocontract's own failures, which its runtime answers on a route's behalf.
//
//ergo:not-an-error
type RuntimeAnswers struct{}

func (GuardAnswers) isAnswerUse()   {}
func (RuntimeAnswers) isAnswerUse() {}

// SharedFailureType is the sealed type the runtime declares holding every failure it answers on a
// route's behalf.
const SharedFailureType = "SharedFailure"

// GoName is a type a package declares: the package's import path and the type's name.
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

// Guarded is a route served behind a guard, which is handed Argument.
//
//ergo:not-an-error
type Guarded struct {
	Guard    Guarding
	Argument string
}

func (Unguarded) isRouteGuard() {}
func (Guarded) isRouteGuard()   {}

// Route is one route as the generated code serves it: its endpoint name, method, path, handler,
// guard, input and outcome, and how its input is read.
type Route struct {
	Name    string
	Method  description.Method
	Path    description.RoutePath
	Handler Handler
	Guard   RouteGuard
	Input   GoName
	Outcome GoName
	Params  []Param
	Query   []Param
	Body    BodyRead
}

// Param is one input field read from a path segment or a query parameter: its name in the
// request, its Go field, how its text is read and the field's Go type, which the generated code
// can name and its text is read as.
type Param struct {
	Name    string
	GoField string
	Text    TextKind
	GoType  types.Type
}

// TextKind is how a parameter's text is read.
//
//sumtype:decl
type TextKind interface{ isTextKind() }

// TextAsIs is text read as it is.
//
//ergo:not-an-error
type TextAsIs struct{}

// TextWhole is text read as a whole number of the width the description gives the field's integer
// type.
//
//ergo:not-an-error
type TextWhole struct{ Width description.IntegerWidth }

// TextTruth is text read as a boolean.
//
//ergo:not-an-error
type TextTruth struct{}

// TextLiteral is text that must be one of the literals the description gives the field.
//
//ergo:not-an-error
type TextLiteral struct{ Literals description.Literals }

func (TextAsIs) isTextKind()    {}
func (TextWhole) isTextKind()   {}
func (TextTruth) isTextKind()   {}
func (TextLiteral) isTextKind() {}

// BodyFieldName is the name of the input field a route reads the JSON body into.
const BodyFieldName = "Body"

// BodyRead is whether a route reads its input's Body field from the JSON body.
//
//sumtype:decl
type BodyRead interface{ isBodyRead() }

// NoBodyRead is a route that reads no body.
//
//ergo:not-an-error
type NoBodyRead struct{}

// BodyFieldRead is a route that reads the JSON body as Value into its input's Body field.
//
//ergo:not-an-error
type BodyFieldRead struct{ Value BodyValue }

func (NoBodyRead) isBodyRead()    {}
func (BodyFieldRead) isBodyRead() {}

// BodyValue is one JSON value of a request's body or an answer: the Go type the generated code
// reads or writes it as, and its shape, which decides how.
type BodyValue struct {
	GoType types.Type
	Shape  BodyShape
}

// BodyShape is how the generated code reads one JSON value of a body. Every constraint a shape
// enforces is the value the description declares for it.
//
//sumtype:decl
type BodyShape interface{ isBodyShape() }

// TextShape is a string read as the field's text type.
//
//ergo:not-an-error
type TextShape struct{}

// WholeShape is a whole number of Width, read as the field's integer type.
//
//ergo:not-an-error
type WholeShape struct{ Width description.IntegerWidth }

// NumberShape is a number read to fit the field's floating-point type.
//
//ergo:not-an-error
type NumberShape struct{}

// TruthShape is true or false.
//
//ergo:not-an-error
type TruthShape struct{}

// InstantShape is an RFC 3339 time read as a time.Time in UTC.
//
//ergo:not-an-error
type InstantShape struct{}

// LiteralShape is a string that must be one of Literals.
//
//ergo:not-an-error
type LiteralShape struct{ Literals description.Literals }

// ListShape is an array read as a slice, each item as Item.
//
//ergo:not-an-error
type ListShape struct{ Item BodyValue }

// ArrayShape is an array of exactly Length items read as a Go array, each item as Item. Length is
// the description's FixedList length.
//
//ergo:not-an-error
type ArrayShape struct {
	Length int64
	Item   BodyValue
}

// DictionaryShape is an object read as a map: each key one Key takes, read as KeyType, and each
// value read as Value.
//
//ergo:not-an-error
type DictionaryShape struct {
	Key     description.DictionaryKey
	KeyType types.Type
	Value   BodyValue
}

// NullableShape is null read as an empty mo.Option, and any other value as Inner.
//
//ergo:not-an-error
type NullableShape struct{ Inner BodyValue }

// StructShape is an object holding exactly Fields, each once.
//
//ergo:not-an-error
type StructShape struct{ Fields []BodyStructField }

func (TextShape) isBodyShape()       {}
func (WholeShape) isBodyShape()      {}
func (NumberShape) isBodyShape()     {}
func (TruthShape) isBodyShape()      {}
func (InstantShape) isBodyShape()    {}
func (LiteralShape) isBodyShape()    {}
func (ListShape) isBodyShape()       {}
func (ArrayShape) isBodyShape()      {}
func (DictionaryShape) isBodyShape() {}
func (NullableShape) isBodyShape()   {}
func (*StructShape) isBodyShape()    {}

// BodyStructField is one field of a struct a body holds: its Go field, the name the body writes it
// under, and its value.
type BodyStructField struct {
	GoField string
	Name    string
	Value   BodyValue
}

// Values is value and its nested values in declaration order. A recursive record's fields are
// visited once even when several fields refer to it.
func (value BodyValue) Values() []BodyValue {
	return value.values(map[*StructShape]struct{}{})
}

func (value BodyValue) values(visited map[*StructShape]struct{}) []BodyValue {
	values := []BodyValue{value}
	switch shape := value.Shape.(type) {
	case ListShape:
		return append(values, shape.Item.values(visited)...)
	case ArrayShape:
		return append(values, shape.Item.values(visited)...)
	case DictionaryShape:
		return append(values, shape.Value.values(visited)...)
	case NullableShape:
		return append(values, shape.Inner.values(visited)...)
	case *StructShape:
		if _, seen := visited[shape]; seen {
			return values
		}
		visited[shape] = struct{}{}
		for _, field := range shape.Fields {
			values = append(values, field.Value.values(visited)...)
		}
	case TextShape, WholeShape, NumberShape, TruthShape, InstantShape, LiteralShape, nil:
	}
	return values
}

// OptionPath is the import path of the package declaring Option, the type a field that may hold
// null is written as, and the type the generated code keeps each member of a body's struct in until
// it is read.
const OptionPath = "github.com/samber/mo"

// Member is one member of an outcome or a guard's refusal: its Go type, whose name is its tag, and
// how it answers, and once each Go type its answer holds that encoding/json writes otherwise than
// the description declares, each before the types it holds.
type Member struct {
	Go      GoName
	Answer  description.Answer
	Written []WrittenType
}

// WrittenType is a Go type an answer holds that encoding/json writes otherwise than the description
// declares, for which the generated code hands the runtime a marshaler.
//
//sumtype:decl
type WrittenType interface{ isWrittenType() }

// DecimalTextType is a whole number type of Width, whose wire is decimal text, which encoding/json
// writes as a JSON number.
//
//ergo:not-an-error
type DecimalTextType struct {
	GoType types.Type
	Width  description.IntegerWidth
}

// NullableType is the mo.Option holding Inner, a value that holds a DecimalTextType. mo.Option's
// own MarshalJSON writes its value with encoding/json's first version, which no marshaler reaches.
//
//ergo:not-an-error
type NullableType struct{ Inner types.Type }

func (DecimalTextType) isWrittenType() {}
func (NullableType) isWrittenType()    {}

// NamedType is the Go type the generated code names to write written.
func NamedType(written WrittenType) types.Type {
	switch typed := written.(type) {
	case DecimalTextType:
		return typed.GoType
	case NullableType:
		return typed.Inner
	case nil:
	}
	return types.Typ[types.Invalid]
}

// WrittenTypes is once each Go type value holds that encoding/json writes otherwise than the
// description declares, each before the types it holds.
func WrittenTypes(value BodyValue) []WrittenType {
	written := []WrittenType{}
	for _, held := range value.Values() {
		switch shape := held.Shape.(type) {
		case WholeShape:
			switch shape.Width.Wire().(type) {
			case description.DecimalTextWire:
				written = append(written, DecimalTextType{GoType: held.GoType, Width: shape.Width})
			case description.NumberWire, nil:
			}
		case NullableShape:
			if slices.ContainsFunc(shape.Inner.Values(), usesDecimalMarshal) {
				written = append(written, NullableType{Inner: shape.Inner.GoType})
			}
		case TextShape, NumberShape, TruthShape, InstantShape, LiteralShape, ListShape, ArrayShape, DictionaryShape, *StructShape, nil:
		}
	}
	once := []WrittenType{}
	for _, each := range written {
		if !slices.ContainsFunc(once, func(kept WrittenType) bool { return sameWrittenType(kept, each) }) {
			once = append(once, each)
		}
	}
	return once
}

func usesDecimalMarshal(value BodyValue) bool {
	if whole, isWhole := value.Shape.(WholeShape); isWhole {
		_, decimal := whole.Width.Wire().(description.DecimalTextWire)
		return decimal
	}
	return false
}

// sameWrittenType is whether left and right hand the runtime a marshaler for the same Go type.
func sameWrittenType(left, right WrittenType) bool {
	switch typed := left.(type) {
	case DecimalTextType:
		other, isDecimal := right.(DecimalTextType)
		return isDecimal && types.Identical(typed.GoType, other.GoType)
	case NullableType:
		other, isNullable := right.(NullableType)
		return isNullable && types.Identical(typed.Inner, other.Inner)
	case nil:
	}
	return false
}
