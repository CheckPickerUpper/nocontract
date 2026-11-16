package reading

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fatih/structtag"
	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// routeEntry is one route read whole: the package and handler declaring it, its endpoint as the
// API declares it, the route its generated code serves, and how its outcome is answered.
type routeEntry struct {
	loaded   *packages.Package
	position token.Pos
	endpoint assembling.DeclaredEndpoint
	route    serverplan.Route
	outcome  serverplan.OutcomeAnswer
}

// handlerDeclaration is a function declaration of the project carrying a //nocontract:route line.
type handlerDeclaration struct {
	loaded   *packages.Package
	function *ast.FuncDecl
	doc      *ast.CommentGroup
}

// routeHandlers is every function of the project's own files carrying a //nocontract:route line.
func routeHandlers(index projectIndex) []handlerDeclaration {
	handlers := []handlerDeclaration{}
	for _, own := range index.own {
		for _, declaration := range own.file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction {
				continue
			}
			doc := mo.Fold(mo.EmptyableToOption(function.Doc), func(written *ast.CommentGroup) *ast.CommentGroup { return written },
				func(error) *ast.CommentGroup { return emptyComment() })
			switch writtenLines(doc, routeKind{}).(type) {
			case oneLine, repeatedLines:
				handlers = append(handlers, handlerDeclaration{loaded: own.loaded, function: function, doc: doc})
			case noLine, nil:
			}
		}
	}
	return handlers
}

// readRoutes reads every route the project declares.
func readRoutes(index projectIndex, guards guardTable) []reading[routeEntry] {
	routes := []reading[routeEntry]{}
	for _, handler := range routeHandlers(index) {
		routes = append(routes, readRoute(index, guards, handler))
	}
	return routes
}

// routeLine is a route directive read as its method and path.
type routeLine struct {
	method description.Method
	path   string
}

// readRouteLine reads "METHOD /path", where METHOD is one a route may answer.
func readRouteLine(text string) mo.Option[routeLine] {
	words := strings.Fields(text)
	if len(words) != 2 || !strings.HasPrefix(words[1], "/") {
		return mo.None[routeLine]()
	}
	return mapOption(description.ParseMethod(words[0]), func(method description.Method) routeLine { return routeLine{method: method, path: words[1]} })
}

// endpointName is a handler's name with its first letter in lower case.
func endpointName(handler string) string {
	first, size := utf8.DecodeRuneInString(handler)
	return string(unicode.ToLower(first)) + handler[size:]
}

// signatureReading is a handler's signature read as its input and outcome, or refused.
//
//sumtype:decl
type signatureReading interface{ isSignatureReading() }

// handlerTypes is a handler's input and outcome types, read from its signature, and what answers
// the route: the function, or the method of its receiver type.
//
//ergo:not-an-error
type handlerTypes struct {
	input    types.Type
	outcome  types.Type
	handler  serverplan.Handler
	receiver mo.Option[*types.TypeName]
}

func (handlerTypes) isSignatureReading()                {}
func (HandlerSignatureUnsupported) isSignatureReading() {}

// readSignature is the input and outcome of a func(context.Context, Input) Outcome, or why the
// handler is not one.
func readSignature(function *ast.FuncDecl, signature *types.Signature) signatureReading {
	refused := func(flaw SignatureFlaw) signatureReading {
		return HandlerSignatureUnsupported{Handler: function.Name.Name, Flaw: flaw}
	}
	switch {
	case signature.TypeParams().Len() > 0 || signature.RecvTypeParams().Len() > 0:
		return refused(TypeParametersDeclared{})
	case signature.Params().Len() != 2 || signature.Results().Len() != 1:
		return refused(WrongShape{})
	}
	context, isNamed := types.Unalias(signature.Params().At(0).Type()).(*types.Named)
	if !isNamed || packagePath(context.Obj()) != "context" || context.Obj().Name() != "Context" {
		return refused(ContextMissing{})
	}
	read := handlerTypes{
		input: signature.Params().At(1).Type(), outcome: signature.Results().At(0).Type(),
		handler: serverplan.FunctionHandler{Name: function.Name.Name}, receiver: mo.None[*types.TypeName](),
	}
	return mo.Fold(mo.EmptyableToOption(signature.Recv()), func(receiver *types.Var) signatureReading {
		named, isNamedReceiver := receiver.Type().(*types.Named)
		if !isNamedReceiver {
			return refused(PointerReceiver{})
		}
		return handlerTypes{
			input: read.input, outcome: read.outcome, handler: serverplan.MethodHandler{Receiver: named.Obj().Name(), Method: function.Name.Name},
			receiver: mo.Some(named.Obj()),
		}
	}, func(error) signatureReading { return read })
}

// readHandlerTypes reads a handler's signature, refusing one that is not a function of a context and
// an input returning an outcome, or a method whose receiver the routes cannot be served with.
func readHandlerTypes(index projectIndex, handler handlerDeclaration) reading[handlerTypes] {
	position := handler.function.Name.Pos()
	object, isFunction := handler.loaded.TypesInfo.Defs[handler.function.Name].(*types.Func)
	if !isFunction {
		return notRead[handlerTypes](index.refused(position, HandlerSignatureUnsupported{Handler: handler.function.Name.Name, Flaw: NotAFunction{}}))
	}
	signature, isSignature := object.Type().(*types.Signature)
	if !isSignature {
		return notRead[handlerTypes](index.refused(position, HandlerSignatureUnsupported{Handler: handler.function.Name.Name, Flaw: NotAFunction{}}))
	}
	switch read := readSignature(handler.function, signature).(type) {
	case handlerTypes:
		return servedHandler(index, handler.loaded, read, position)
	case HandlerSignatureUnsupported:
		return notRead[handlerTypes](index.refused(position, read))
	case nil:
	}
	return notRead[handlerTypes](nothingFound())
}

// servedHandler is the handler when the routes can be served with it, refusing a method whose
// receiver type is declared in another package or unexported, which code mounting the routes
// cannot name.
func servedHandler(index projectIndex, loaded *packages.Package, handler handlerTypes, position token.Pos) reading[handlerTypes] {
	return mo.Fold(handler.receiver, func(receiver *types.TypeName) reading[handlerTypes] {
		switch {
		case packagePath(receiver) != loaded.PkgPath:
			return notRead[handlerTypes](index.refused(position, ReceiverUnsupported{Receiver: receiver.Name(), Flaw: ReceiverElsewhere{}}))
		case !receiver.Exported():
			return notRead[handlerTypes](index.refused(position, ReceiverUnsupported{Receiver: receiver.Name(), Flaw: ReceiverUnexported{}}))
		}
		return readAs(handler, nothingFound())
	}, func(error) reading[handlerTypes] { return readAs(handler, nothingFound()) })
}

// readRoute reads one handler's route whole: its line, signature, guard, input and outcome.
func readRoute(index projectIndex, guards guardTable, handler handlerDeclaration) reading[routeEntry] {
	position := handler.function.Name.Pos()
	declared := paired(paired(readRouteDirective(index, handler), readHandlerTypes(index, handler)), readRouteGuard(index, guards, writtenLines(handler.doc, guardKind{})))
	return alongside(andThen(declared, func(read pair[pair[routeLine, handlerTypes], routeGuard]) reading[routeEntry] {
		route, signature, guard := read.first.first, read.first.second, read.second
		parts := paired(readInput(index, handler.loaded, signature.input, position), readOutcome(index, handler.loaded, signature.outcome, position))
		return mapped(parts, func(parts pair[inputShape, outcomeShape]) routeEntry {
			return readEntry(index, handler, routeParts{route: route, handler: signature, guard: guard, input: parts.first, outcome: parts.second})
		})
	}), packageRead(handler.loaded.PkgPath))
}

// routeParts is everything one route was read from.
type routeParts struct {
	route   routeLine
	handler handlerTypes
	guard   routeGuard
	input   inputShape
	outcome outcomeShape
}

// readEntry is a route read whole, as its endpoint, served route and outcome answer.
func readEntry(index projectIndex, handler handlerDeclaration, parts routeParts) routeEntry {
	name := endpointName(handler.function.Name.Name)
	answered := answeredBy(parts.outcome.plans)
	return routeEntry{
		loaded: handler.loaded, position: handler.function.Name.Pos(),
		endpoint: assembling.DeclaredEndpoint{
			Name: name, Method: parts.route.method, Path: parts.route.path, Params: parameterFields(parts.input.params),
			Query: parameterFields(parts.input.query), Body: parts.input.body, Successes: answered.successes,
			Failures: slices.Concat(answered.failures, parts.guard.members), Position: index.placed(handler.function.Name.Pos()),
		},
		route: serverplan.Route{
			Name: name, Method: parts.route.method, Path: parts.route.path, Handler: parts.handler.handler, Guard: parts.guard.plan,
			Input: parts.input.name, Outcome: parts.outcome.name, Params: parameterPlans(parts.input.params), Query: parameterPlans(parts.input.query),
			Body: bodyRead(parts.input.body),
		},
		outcome: serverplan.OutcomeAnswer{Outcome: parts.outcome.name, Members: parts.outcome.plans},
	}
}

// readRouteDirective reads a handler's one //nocontract:route line.
func readRouteDirective(index projectIndex, handler handlerDeclaration) reading[routeLine] {
	switch lines := writtenLines(handler.doc, routeKind{}).(type) {
	case oneLine:
		return mo.Fold(readRouteLine(lines.line.argument), func(route routeLine) reading[routeLine] {
			return readAs(route, nothingFound())
		}, func(error) reading[routeLine] {
			return notRead[routeLine](index.refused(lines.line.position, RouteDirectiveMalformed{Text: lines.line.argument}))
		})
	case repeatedLines:
		return notRead[routeLine](index.refused(lines.second.position, RouteDirectiveRepeated{Handler: handler.function.Name.Name}))
	case noLine, nil:
	}
	return notRead[routeLine](nothingFound())
}

// bodyRead is whether the generated code decodes the input's Body field.
func bodyRead(body description.Body) serverplan.BodyRead {
	switch body.(type) {
	case description.JSONBody:
		return serverplan.BodyFieldRead{}
	case description.NoBody, nil:
	}
	return serverplan.NoBodyRead{}
}

// answered is an outcome's members by how they answer: the names of its successes and of its
// failures, each in declaration order.
type answered struct {
	successes []string
	failures  []string
}

// answeredBy sorts an outcome's members into its successes and its failures.
func answeredBy(members []serverplan.Member) answered {
	sorted := answered{successes: []string{}, failures: []string{}}
	for _, member := range members {
		switch member.Answer.(type) {
		case description.Succeeds:
			sorted.successes = append(sorted.successes, member.Go.Name)
		case description.Fails:
			sorted.failures = append(sorted.failures, member.Go.Name)
		case nil:
		}
	}
	return sorted
}

// reachableName is the Go name the route's generated code in package from refers to a type by,
// refusing an unexported type of another package, which that code cannot name.
func reachableName(index projectIndex, object *types.TypeName, from string, position token.Pos) reading[serverplan.GoName] {
	return mo.Fold(mo.EmptyableToOption(object.Pkg()), func(owner *types.Package) reading[serverplan.GoName] {
		if !object.Exported() && owner.Path() != from {
			return notRead[serverplan.GoName](index.refused(position, TypeNotReachable{Type: owner.Path() + "." + object.Name()}))
		}
		return readAs(serverplan.GoName{Package: owner.Path(), Name: object.Name()}, nothingFound())
	}, func(error) reading[serverplan.GoName] {
		// A named type no package declares is one Go declares, such as error, which no input,
		// outcome, member or parameter can be.
		return notRead[serverplan.GoName](index.refused(position, TypeNotReachable{Type: object.Name()}))
	})
}

// goName is the Go name of a type the project's syntax declares.
func goName(declared declaredType) serverplan.GoName {
	return serverplan.GoName{Package: declared.loaded.PkgPath, Name: declared.object.Name()}
}

// parameter is one path or query field: its JSON field and how the generated code reads it.
type parameter struct {
	field description.Field
	plan  serverplan.Param
}

// parameterFields is each parameter's JSON field.
func parameterFields(params []parameter) []description.Field {
	fields := []description.Field{}
	for _, param := range params {
		fields = append(fields, param.field)
	}
	return fields
}

// parameterPlans is how the generated code reads each parameter.
func parameterPlans(params []parameter) []serverplan.Param {
	plans := []serverplan.Param{}
	for _, param := range params {
		plans = append(plans, param.plan)
	}
	return plans
}

// inputShape is a route's input type and what it reads from its request.
type inputShape struct {
	name   serverplan.GoName
	params []parameter
	query  []parameter
	body   description.Body
}

// inputField is where one input field is read from, decided by one function.
//
//sumtype:decl
type inputField interface{ isInputField() }

// pathField reads the path segment it names.
//
//ergo:not-an-error
type pathField struct{ name string }

// queryField reads the query parameter it names.
//
//ergo:not-an-error
type queryField struct{ name string }

// bodyField reads the JSON body.
//
//ergo:not-an-error
type bodyField struct{}

// embeddedInputField is an embedded field, which names no part of the request.
//
//ergo:not-an-error
type embeddedInputField struct{}

// unplacedField is a field with no place in the request.
//
//ergo:not-an-error
type unplacedField struct{}

func (pathField) isInputField()          {}
func (queryField) isInputField()         {}
func (bodyField) isInputField()          {}
func (embeddedInputField) isInputField() {}
func (unplacedField) isInputField()      {}

// fieldTag is a struct field's tag, as Go's type checker gives it.
type fieldTag string

// value is the tag's value for key, when the tag carries it.
func (tag fieldTag) value(key string) mo.Option[string] {
	return mo.Fold(mo.TupleToResult(structtag.Parse(string(tag))), func(tags *structtag.Tags) mo.Option[string] {
		return mo.Fold(mo.TupleToResult(tags.Get(key)), func(found *structtag.Tag) mo.Option[string] { return mo.Some(found.Value()) },
			func(error) mo.Option[string] { return mo.None[string]() })
	}, func(error) mo.Option[string] { return mo.None[string]() })
}

// inputFieldPlace is where the generated code reads an input field from.
func inputFieldPlace(field indexedField) inputField {
	switch {
	case field.variable.Embedded():
		return embeddedInputField{}
	case !field.variable.Exported():
		return unplacedField{}
	}
	return mo.Fold(fieldTag(field.tag).value("path"), func(name string) inputField { return pathField{name: name} }, func(error) inputField {
		return mo.Fold(fieldTag(field.tag).value("query"), func(name string) inputField { return queryField{name: name} }, func(error) inputField {
			if field.variable.Name() == "Body" {
				return bodyField{}
			}
			return unplacedField{}
		})
	})
}

// placedField is one input field read where it is placed.
//
//sumtype:decl
type placedField interface{ isPlacedField() }

// pathRead is a field read from a path segment.
//
//ergo:not-an-error
type pathRead struct{ read parameter }

// queryRead is a field read from a query parameter.
//
//ergo:not-an-error
type queryRead struct{ read parameter }

// bodyFieldRead is the field read from the JSON body.
//
//ergo:not-an-error
type bodyFieldRead struct{ schema description.Schema }

func (pathRead) isPlacedField()      {}
func (queryRead) isPlacedField()     {}
func (bodyFieldRead) isPlacedField() {}

// readInput reads a route's input: a named struct each of whose fields is read from one part of
// the request.
func readInput(index projectIndex, loaded *packages.Package, input types.Type, position token.Pos) reading[inputShape] {
	named, isNamed := types.Unalias(input).(*types.Named)
	if !isNamed {
		return notRead[inputShape](index.refused(position, InputNotStruct{Input: types.TypeString(input, nil)}))
	}
	fields, isStruct := named.Underlying().(*types.Struct)
	if !isStruct {
		return notRead[inputShape](index.refused(position, InputNotStruct{Input: types.TypeString(input, nil)}))
	}
	return alongside(andThen(reachableName(index, named.Obj(), loaded.PkgPath, position), func(name serverplan.GoName) reading[inputShape] {
		placed := collected(indexedFields(fields), func(field indexedField) reading[placedField] { return readPlacedField(index, loaded, field) })
		return mapped(placed, func(placed []placedField) inputShape { return sortedInput(name, placed) })
	}), packageRead(packagePath(named.Obj())))
}

// readPlacedField reads one input field where it is placed.
func readPlacedField(index projectIndex, loaded *packages.Package, field indexedField) reading[placedField] {
	position := field.variable.Pos()
	switch place := inputFieldPlace(field).(type) {
	case pathField:
		return mapped(readParameter(index, loaded, field.variable, place.name), func(read parameter) placedField { return pathRead{read: read} })
	case queryField:
		return mapped(readParameter(index, loaded, field.variable, place.name), func(read parameter) placedField { return queryRead{read: read} })
	case bodyField:
		return mapped(readSchema(index, field.variable.Type(), position, []typeKey{}), func(schema description.Schema) placedField { return bodyFieldRead{schema: schema} })
	case embeddedInputField:
		return notRead[placedField](index.refused(position, EmbeddedFieldUnsupported{Field: field.variable.Name()}))
	case unplacedField, nil:
	}
	return notRead[placedField](index.refused(position, InputFieldUnread{Field: field.variable.Name()}))
}

// sortedInput is an input's fields, sorted by where each is read.
func sortedInput(name serverplan.GoName, placed []placedField) inputShape {
	shape := inputShape{name: name, params: []parameter{}, query: []parameter{}, body: description.NoBody{}}
	for _, field := range placed {
		switch read := field.(type) {
		case pathRead:
			shape.params = append(shape.params, read.read)
		case queryRead:
			shape.query = append(shape.query, read.read)
		case bodyFieldRead:
			shape.body = description.JSONBody{Schema: read.schema}
		case nil:
		}
	}
	return shape
}

// readParameter reads a path or query field, whose text the generated code parses by its type.
func readParameter(index projectIndex, loaded *packages.Package, field *types.Var, name string) reading[parameter] {
	unsupported := notRead[parameter](index.refused(field.Pos(), ParameterTypeUnsupported{Field: field.Name(), Type: types.TypeString(field.Type(), nil)}))
	basic, isBasic := field.Type().Underlying().(*types.Basic)
	if !isBasic {
		return unsupported
	}
	goType := readAs[serverplan.ParamType](serverplan.PredeclaredParamType{Name: basic.Name()}, nothingFound())
	if named, isNamed := types.Unalias(field.Type()).(*types.Named); isNamed {
		goType = mapped(reachableName(index, named.Obj(), loaded.PkgPath, field.Pos()), func(name serverplan.GoName) serverplan.ParamType {
			return serverplan.DeclaredParamType{Name: name}
		})
	}
	return andThen(goType, func(goType serverplan.ParamType) reading[parameter] {
		return andThen(readSchema(index, field.Type(), field.Pos(), []typeKey{}), func(schema description.Schema) reading[parameter] {
			return mo.Fold(textKind(basic, schema), func(kind serverplan.TextKind) reading[parameter] {
				return readAs(parameter{
					field: description.Field{Name: name, Schema: schema},
					plan:  serverplan.Param{Name: name, GoField: field.Name(), Text: kind, GoType: goType},
				}, nothingFound())
			}, func(error) reading[parameter] { return unsupported })
		})
	})
}

// textKind is how the generated code parses a parameter's text into its schema.
func textKind(basic *types.Basic, schema description.Schema) mo.Option[serverplan.TextKind] {
	switch typed := schema.(type) {
	case description.Literals:
		return mo.Some[serverplan.TextKind](serverplan.TextLiteral{Values: typed.Values})
	case description.Text:
		return mo.Some[serverplan.TextKind](serverplan.TextAsIs{})
	case description.Truth:
		return mo.Some[serverplan.TextKind](serverplan.TextTruth{})
	case description.Integer:
		return mo.Some(integerKind(basic))
	case description.Finite, description.Instant, description.List, description.Dictionary, description.Nullable,
		description.StructRef, description.InlineStruct, nil:
	}
	return mo.None[serverplan.TextKind]()
}

// integerKind is how a whole number of basic's size is parsed: int and uint at the platform's
// size, which strconv takes as 0 bits, and every other at the size Go gives it.
func integerKind(basic *types.Basic) serverplan.TextKind {
	bits := int(types.SizesFor("gc", "amd64").Sizeof(basic) * 8)
	switch basic.Kind() {
	case types.Int:
		return serverplan.TextSigned{Bits: 0}
	case types.Uint, types.Uintptr:
		return serverplan.TextUnsigned{Bits: 0}
	case types.Int8, types.Int16, types.Int32, types.Int64, types.UntypedInt, types.UntypedRune:
		return serverplan.TextSigned{Bits: bits}
	case types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		return serverplan.TextUnsigned{Bits: bits}
	case types.Invalid, types.Bool, types.Float32, types.Float64, types.Complex64, types.Complex128, types.String, types.UnsafePointer,
		types.UntypedBool, types.UntypedFloat, types.UntypedComplex, types.UntypedString, types.UntypedNil:
	}
	return serverplan.TextSigned{Bits: bits}
}
