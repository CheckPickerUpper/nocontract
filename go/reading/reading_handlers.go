package reading

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fatih/structtag"
	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// routeEntry is one route read whole: the package and handler declaring it, its endpoint, the
// route its generated code serves, and how its outcome is answered.
type routeEntry struct {
	loaded   *packages.Package
	position token.Pos
	endpoint description.Endpoint
	route    serverplan.Route
	outcome  serverplan.OutcomeAnswer
}

// handlerDeclaration is a function declaration of the project carrying a //nocontract:route line.
type handlerDeclaration struct {
	loaded   *packages.Package
	function *ast.FuncDecl
	doc      *ast.CommentGroup
}

// handlersOf is every function of the project's own files carrying a //nocontract:route line.
func handlersOf(index projectIndex) []handlerDeclaration {
	handlers := []handlerDeclaration{}
	for _, own := range index.own {
		for _, declaration := range own.file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction {
				continue
			}
			doc := mo.Fold(mo.EmptyableToOption(function.Doc), func(written *ast.CommentGroup) *ast.CommentGroup { return written },
				func(error) *ast.CommentGroup { return emptyComment() })
			switch linesOf(doc, routeKind{}).(type) {
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
	for _, handler := range handlersOf(index) {
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

// signatureOf is the input and outcome of a func(context.Context, Input) Outcome, or why the
// handler is not one.
func signatureOf(function *ast.FuncDecl, signature *types.Signature) signatureReading {
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
	switch read := signatureOf(handler.function, signature).(type) {
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
	declared := paired(paired(readRouteDirective(index, handler), readHandlerTypes(index, handler)), readRouteGuard(index, guards, linesOf(handler.doc, guardKind{})))
	return alongside(andThen(declared, func(read pair[pair[routeLine, handlerTypes], routeGuard]) reading[routeEntry] {
		route, signature, guard := read.first.first, read.first.second, read.second
		input := readInput(index, handler.loaded, signature.input, route.method, position)
		parts := paired(paired(pathParameters(index, route.path, position), input), readOutcome(index, handler.loaded, signature.outcome, position))
		return andThen(parts, func(parts pair[pair[[]string, inputShape], outcomeShape]) reading[routeEntry] {
			outcome := parts.second
			return mapped(matchPathParameters(index, parts.first.first, parts.first.second, position), func(input inputShape) routeEntry {
				return entryOf(index, handler, routeParts{route: route, handler: signature, guard: guard, input: input, outcome: outcome})
			})
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

// entryOf is a route read whole, as its endpoint, served route and outcome answer.
func entryOf(index projectIndex, handler handlerDeclaration, parts routeParts) routeEntry {
	name := endpointName(handler.function.Name.Name)
	answered := answeredBy(parts.outcome.plans)
	return routeEntry{
		loaded: handler.loaded, position: handler.function.Name.Pos(),
		endpoint: description.Endpoint{
			Name: name, Method: parts.route.method, Path: parts.route.path, Params: fieldsOfParameters(parts.input.params),
			Query: fieldsOfParameters(parts.input.query), Body: parts.input.body, Successes: answered.successes,
			Failures: slices.Concat(answered.failures, refusalsOf(index, parts.input, parts.guard)),
		},
		route: serverplan.Route{
			Name: name, Method: parts.route.method, Path: parts.route.path, Handler: parts.handler.handler, Guard: parts.guard.plan,
			Input: parts.input.name, Outcome: parts.outcome.name, Params: plansOf(parts.input.params), Query: plansOf(parts.input.query),
			Body: bodyRead(parts.input.body),
		},
		outcome: serverplan.OutcomeAnswer{Outcome: parts.outcome.name, Members: parts.outcome.plans},
	}
}

// readRouteDirective reads a handler's one //nocontract:route line.
func readRouteDirective(index projectIndex, handler handlerDeclaration) reading[routeLine] {
	switch lines := linesOf(handler.doc, routeKind{}).(type) {
	case oneLine:
		return mo.Fold(readRouteLine(lines.line.argument), func(route routeLine) reading[routeLine] {
			return readAs(route, nothingFound())
		}, func(error) reading[routeLine] {
			return notRead[routeLine](refusedAt(index.position(lines.line.position), RouteDirectiveMalformed{Text: lines.line.argument}))
		})
	case repeatedLines:
		return notRead[routeLine](refusedAt(index.position(lines.second.position), RouteDirectiveMalformed{Text: "a handler declares exactly one route"}))
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

// refusalsOf is every member that can answer a route outside its handler: its guard's refusals,
// RequestRefused when its input reads any part of the request, and AnswerNotWritten.
func refusalsOf(index projectIndex, read inputShape, guard routeGuard) []string {
	refusals := slices.Clone(guard.members)
	switch read.request.(type) {
	case requestRead:
		refusals = append(refusals, index.shared.requestRefused)
	case requestUnread, nil:
	}
	return append(refusals, index.shared.answerNotWritten)
}

// reachableName is the Go name the route's generated code in package from refers to a type by,
// refusing an unexported type of another package, which that code cannot name.
func reachableName(index projectIndex, object *types.TypeName, from string, position token.Pos) reading[serverplan.GoName] {
	if object.Exported() || packagePath(object) == from || packagePath(object) == "" {
		return readAs(goName(object), nothingFound())
	}
	return notRead[serverplan.GoName](index.refused(position, TypeNotReachable{Type: packagePath(object) + "." + object.Name()}))
}

// goName is the Go name of a named or predeclared type.
func goName(object *types.TypeName) serverplan.GoName {
	return serverplan.GoName{Package: packagePath(object), Name: object.Name()}
}

// pathParameters is every {name} segment of a path, refusing the segments Effect has no form for.
func pathParameters(index projectIndex, path string, position token.Pos) reading[[]string] {
	segments := []string{}
	for segment := range strings.SplitSeq(path, "/") {
		if inner, isParameter := strings.CutPrefix(segment, "{"); isParameter {
			segments = append(segments, inner)
		}
	}
	return collected(segments, func(inner string) reading[string] {
		name := strings.TrimSuffix(inner, "}")
		if name == "$" || strings.HasSuffix(name, "...") {
			return notRead[string](index.refused(position, PathWildcardUnsupported{Segment: "{" + inner}))
		}
		return readAs(name, nothingFound())
	})
}

// matchPathParameters binds each segment of the path to the input field that reads it, refusing a
// segment no field reads and a field reading a segment the path does not have. The input's path
// fields come back in the order the path names them.
func matchPathParameters(index projectIndex, segments []string, input inputShape, position token.Pos) reading[inputShape] {
	unbound := map[string]parameter{}
	for _, param := range input.params {
		unbound[param.field.Name] = param
	}
	bound := collected(segments, func(segment string) reading[parameter] {
		param, reads := unbound[segment]
		if !reads {
			return notRead[parameter](index.refused(position, PathParameterUndeclared{Parameter: segment}))
		}
		delete(unbound, segment)
		return readAs(param, nothingFound())
	})
	unused := collected(slices.Sorted(maps.Keys(unbound)), func(name string) reading[string] {
		return notRead[string](index.refused(position, PathFieldUnused{Parameter: name}))
	})
	return mapped(paired(bound, unused), func(read pair[[]parameter, []string]) inputShape {
		return inputShape{name: input.name, params: read.first, query: input.query, body: input.body, request: input.request}
	})
}

// parameter is one path or query field: its JSON field and how the generated code reads it.
type parameter struct {
	field description.Field
	plan  serverplan.Param
}

func fieldsOfParameters(params []parameter) []description.Field {
	fields := []description.Field{}
	for _, param := range params {
		fields = append(fields, param.field)
	}
	return fields
}

func plansOf(params []parameter) []serverplan.Param {
	plans := []serverplan.Param{}
	for _, param := range params {
		plans = append(plans, param.plan)
	}
	return plans
}

// inputShape is a route's input type, what it reads from its request, and whether it reads any of
// it.
type inputShape struct {
	name    serverplan.GoName
	params  []parameter
	query   []parameter
	body    description.Body
	request requestUse
}

// requestUse is whether a route's input reads any part of its request, which it can then refuse.
//
//sumtype:decl
type requestUse interface{ isRequestUse() }

// requestRead is an input reading a path segment, a query parameter or the body.
//
//ergo:not-an-error
type requestRead struct{}

// requestUnread is an input reading nothing from the request.
//
//ergo:not-an-error
type requestUnread struct{}

func (requestRead) isRequestUse()   {}
func (requestUnread) isRequestUse() {}

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

// placeOfInputField is where the generated code reads an input field from.
func placeOfInputField(field indexedField) inputField {
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

func readInput(index projectIndex, loaded *packages.Package, input types.Type, method description.Method, position token.Pos) reading[inputShape] {
	named, isNamed := types.Unalias(input).(*types.Named)
	if !isNamed {
		return notRead[inputShape](index.refused(position, InputNotStruct{Input: types.TypeString(input, nil)}))
	}
	fields, isStruct := named.Underlying().(*types.Struct)
	if !isStruct {
		return notRead[inputShape](index.refused(position, InputNotStruct{Input: types.TypeString(input, nil)}))
	}
	return alongside(andThen(reachableName(index, named.Obj(), loaded.PkgPath, position), func(name serverplan.GoName) reading[inputShape] {
		placed := collected(fieldsOf(fields), func(field indexedField) reading[placedField] { return readPlacedField(index, loaded, field, method) })
		return mapped(placed, func(placed []placedField) inputShape { return shapeOf(name, placed) })
	}), packageRead(packagePath(named.Obj())))
}

// readPlacedField reads one input field where it is placed.
func readPlacedField(index projectIndex, loaded *packages.Package, field indexedField, method description.Method) reading[placedField] {
	position := field.variable.Pos()
	switch place := placeOfInputField(field).(type) {
	case pathField:
		return mapped(readParameter(index, loaded, field.variable, place.name), func(read parameter) placedField { return pathRead{read: read} })
	case queryField:
		return mapped(readParameter(index, loaded, field.variable, place.name), func(read parameter) placedField { return queryRead{read: read} })
	case bodyField:
		switch method.Body().(type) {
		case description.BodyCarried:
			return mapped(schemaOf(index, field.variable.Type(), position, []typeKey{}), func(schema description.Schema) placedField { return bodyFieldRead{schema: schema} })
		case description.BodyAbsent, nil:
		}
		return notRead[placedField](index.refused(position, BodyOnGet{}))
	case embeddedInputField:
		return notRead[placedField](index.refused(position, EmbeddedFieldUnsupported{Field: field.variable.Name()}))
	case unplacedField, nil:
	}
	return notRead[placedField](index.refused(position, InputFieldUnread{Field: field.variable.Name()}))
}

// shapeOf is an input's fields, sorted by where each is read.
func shapeOf(name serverplan.GoName, placed []placedField) inputShape {
	shape := inputShape{name: name, params: []parameter{}, query: []parameter{}, body: description.NoBody{}, request: requestUnread{}}
	for _, field := range placed {
		shape.request = requestRead{}
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
	goType := readAs(serverplan.GoName{Package: "", Name: basic.Name()}, nothingFound())
	if named, isNamed := types.Unalias(field.Type()).(*types.Named); isNamed {
		goType = reachableName(index, named.Obj(), loaded.PkgPath, field.Pos())
	}
	return andThen(goType, func(goType serverplan.GoName) reading[parameter] {
		return andThen(schemaOf(index, field.Type(), field.Pos(), []typeKey{}), func(schema description.Schema) reading[parameter] {
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
