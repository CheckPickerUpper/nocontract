package reading

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
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
	for _, method := range description.Methods() {
		if method.Name() == words[0] {
			return mo.Some(routeLine{method: method, path: words[1]})
		}
	}
	return mo.None[routeLine]()
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
func signatureOf(function *ast.FuncDecl, object *types.Func) signatureReading {
	signature, isSignature := object.Type().(*types.Signature)
	refused := func(flaw SignatureFlaw) signatureReading {
		return HandlerSignatureUnsupported{Handler: function.Name.Name, Flaw: flaw}
	}
	switch {
	case !isSignature:
		return refused(NotAFunction{})
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
		read.handler = serverplan.MethodHandler{Receiver: named.Obj().Name(), Method: function.Name.Name}
		read.receiver = mo.Some(named.Obj())
		return read
	}, func(error) signatureReading { return read })
}

func emptyComment() *ast.CommentGroup { return &ast.CommentGroup{List: []*ast.Comment{}} }

func (reader *projectReader) readHandler(loaded *packages.Package, function *ast.FuncDecl) {
	doc := mo.Fold(mo.EmptyableToOption(function.Doc), func(found *ast.CommentGroup) *ast.CommentGroup { return found },
		func(error) *ast.CommentGroup { return emptyComment() })
	found := directivesIn(doc)
	routes := named(found, routeDirective)
	if len(routes) == 0 {
		return
	}
	reader.read[loaded.PkgPath] = struct{}{}
	object, isFunction := loaded.TypesInfo.Defs[function.Name].(*types.Func)
	if !isFunction || len(routes) > 1 {
		reader.refuse(function.Pos(), RouteDirectiveMalformed{Text: "a handler declares exactly one route"})
		return
	}
	mo.Fold(readRouteLine(routes[0].argument), func(route routeLine) struct{} {
		guard := reader.readRouteGuard(named(found, guardDirective))
		switch read := signatureOf(function, object).(type) {
		case handlerTypes:
			guard.ForEach(func(guarded routeGuard) { reader.readRoute(loaded, function, route, read, guarded) })
		case HandlerSignatureUnsupported:
			reader.refuse(function.Name.Pos(), read)
		case nil:
		}
		return struct{}{}
	}, func(error) struct{} {
		reader.refuse(routes[0].position, RouteDirectiveMalformed{Text: routes[0].argument})
		return struct{}{}
	})
}

func (reader *projectReader) readRoute(loaded *packages.Package, function *ast.FuncDecl, route routeLine, read handlerTypes, guard routeGuard) {
	position := function.Name.Pos()
	reader.servedHandler(loaded, read, position).ForEach(func(handler handlerTypes) {
		reader.readServedRoute(loaded, function, route, handler, guard)
	})
}

func (reader *projectReader) readServedRoute(loaded *packages.Package, function *ast.FuncDecl, route routeLine, handler handlerTypes, guard routeGuard) {
	position := function.Name.Pos()
	pattern := route.method.Name() + " " + route.path
	if first, served := reader.patterns[pattern]; served {
		reader.refuse(position, RouteRepeated{Pattern: pattern, First: first})
		return
	}
	reader.patterns[pattern] = reader.files.Position(position)
	refusedBefore := len(reader.refusals)
	segments := reader.pathParameters(route.path, position)
	input := reader.readInput(loaded, handler.input, route.method, position)
	outcome := reader.readOutcome(loaded, handler.outcome, position)
	input.ForEach(func(read inputShape) { reader.matchPathParameters(segments, read.params, position) })
	mo.Fold(input, func(read inputShape) struct{} {
		mo.Fold(outcome, func(members outcomeShape) struct{} {
			if len(reader.refusals) > refusedBefore {
				return struct{}{}
			}
			name := endpointName(function.Name.Name)
			answered := answeredBy(members.plans)
			reader.addRoute(loaded, function, description.Endpoint{
				Name: name, Method: route.method, Path: route.path, Params: fieldsOf(read.params), Query: fieldsOf(read.query),
				Body: read.body, Successes: answered.successes, Failures: append(answered.failures, reader.refusalsOf(read, guard)...),
			}, serverplan.Route{
				Name: name, Method: route.method, Path: route.path, Handler: handler.handler, Guard: guard.plan,
				Input: read.name, Outcome: members.name, Params: plansOf(read.params), Query: plansOf(read.query),
				Body: bodyRead(read.body), Members: members.plans,
			})
			return struct{}{}
		}, func(error) struct{} { return struct{}{} })
		return struct{}{}
	}, func(error) struct{} { return struct{}{} })
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
func (reader *projectReader) refusalsOf(read inputShape, guard routeGuard) []string {
	refusals := slices.Clone(guard.members)
	switch read.body.(type) {
	case description.JSONBody:
		refusals = append(refusals, reader.shared.requestRefused)
	case description.NoBody, nil:
		if len(read.params) > 0 || len(read.query) > 0 {
			refusals = append(refusals, reader.shared.requestRefused)
		}
	}
	return append(refusals, reader.shared.answerNotWritten)
}

// servedHandler is the handler when the routes can be served with it, refusing a method whose
// receiver type is declared in another package or unexported, which code mounting the routes
// cannot name.
func (reader *projectReader) servedHandler(loaded *packages.Package, handler handlerTypes, position token.Pos) mo.Option[handlerTypes] {
	return mo.Fold(handler.receiver, func(receiver *types.TypeName) mo.Option[handlerTypes] {
		switch {
		case packagePath(receiver) != loaded.PkgPath:
			reader.refuse(position, ReceiverUnsupported{Receiver: receiver.Name(), Flaw: ReceiverElsewhere{}})
			return mo.None[handlerTypes]()
		case !receiver.Exported():
			reader.refuse(position, ReceiverUnsupported{Receiver: receiver.Name(), Flaw: ReceiverUnexported{}})
			return mo.None[handlerTypes]()
		}
		return mo.Some(handler)
	}, func(error) mo.Option[handlerTypes] { return mo.Some(handler) })
}

func (reader *projectReader) addRoute(loaded *packages.Package, function *ast.FuncDecl, endpoint description.Endpoint, route serverplan.Route) {
	position := function.Name.Pos()
	build := reader.buildFor(loaded, position)
	if len(build.group.Endpoints) == 0 {
		reader.reserve(description.GroupClassName(loaded.Name), position)
		reader.refuseTakenNames(loaded, position)
	}
	if first, taken := build.endpoints[endpoint.Name]; taken {
		reader.refuse(position, EndpointNameRepeated{Name: endpoint.Name, First: first})
		return
	}
	build.endpoints[endpoint.Name] = reader.files.Position(position)
	switch handler := route.Handler.(type) {
	case serverplan.MethodHandler:
		reader.addServingField(build, receiverField{name: handler.Receiver}, position)
	case serverplan.FunctionHandler, nil:
	}
	switch guard := route.Guard.(type) {
	case serverplan.Guarded:
		reader.addServingField(build, guardField{name: guard.Name}, position)
	case serverplan.Unguarded, nil:
	}
	build.group.Endpoints = append(build.group.Endpoints, endpoint)
	build.plan.Routes = append(build.plan.Routes, route)
}

// servingField is one field of what a package's routes are served with, named for the receiver
// type or the guard it holds.
//
//sumtype:decl
type servingField interface {
	isServingField()
	fieldName() string
}

// receiverField holds the receiver of the package's method handlers of one type.
//
//ergo:not-an-error
type receiverField struct{ name string }

// guardField holds one guard the package's routes name.
//
//ergo:not-an-error
type guardField struct{ name string }

func (receiverField) isServingField() {}
func (guardField) isServingField()    {}

func (field receiverField) fieldName() string { return field.name }
func (field guardField) fieldName() string    { return field.name }

// addServingField adds a field of what the package's routes are served with once, refusing a
// field whose name another field already holds.
func (reader *projectReader) addServingField(build *groupBuild, field servingField, position token.Pos) {
	if held, taken := build.fields[field.fieldName()]; taken {
		if held != field {
			reader.refuse(position, GeneratedNameTaken{Name: serverplan.ServingType + "." + field.fieldName()})
		}
		return
	}
	build.fields[field.fieldName()] = field
	switch added := field.(type) {
	case receiverField:
		build.plan.Serving.Receivers = append(build.plan.Serving.Receivers, serverplan.GoName{Package: build.plan.Path, Name: added.name})
	case guardField:
		build.plan.Serving.Guards = append(build.plan.Serving.Guards, added.name)
	case nil:
	}
}

// refuseTakenNames refuses a name the generated file declares that the package already declares
// in a file of its own.
func (reader *projectReader) refuseTakenNames(loaded *packages.Package, position token.Pos) {
	for _, name := range serverplan.CalledNames() {
		mo.EmptyableToOption(loaded.Types.Scope().Lookup(name)).ForEach(func(declared types.Object) {
			if filepath.Base(reader.files.Position(declared.Pos()).Filename) != serverplan.FileName {
				reader.refuse(position, GeneratedNameTaken{Name: name})
			}
		})
	}
}

// reachableName is the Go name the route's generated code in package from refers to a type by,
// refusing an unexported type of another package, which that code cannot name.
func (reader *projectReader) reachableName(object *types.TypeName, from string, position token.Pos) mo.Option[serverplan.GoName] {
	if object.Exported() || packagePath(object) == from || packagePath(object) == "" {
		return mo.Some(goName(object))
	}
	reader.refuse(position, TypeNotReachable{Type: packagePath(object) + "." + object.Name()})
	return mo.None[serverplan.GoName]()
}

// goName is the Go name of a named or predeclared type.
func goName(object *types.TypeName) serverplan.GoName {
	return serverplan.GoName{Package: packagePath(object), Name: object.Name()}
}

// pathParameters is every {name} segment of a path, refusing the segments Effect has no form for.
func (reader *projectReader) pathParameters(path string, position token.Pos) []string {
	parameters := []string{}
	for segment := range strings.SplitSeq(path, "/") {
		inner, isParameter := strings.CutPrefix(segment, "{")
		if !isParameter {
			continue
		}
		name := strings.TrimSuffix(inner, "}")
		if name == "$" || strings.HasSuffix(name, "...") {
			reader.refuse(position, PathWildcardUnsupported{Segment: segment})
			continue
		}
		parameters = append(parameters, name)
	}
	return parameters
}

func (reader *projectReader) matchPathParameters(segments []string, params []parameter, position token.Pos) {
	read := []string{}
	for _, param := range params {
		read = append(read, param.field.Name)
		if !slices.Contains(segments, param.field.Name) {
			reader.refuse(position, PathFieldUnused{Parameter: param.field.Name})
		}
	}
	for _, segment := range segments {
		if !slices.Contains(read, segment) {
			reader.refuse(position, PathParameterUndeclared{Parameter: segment})
		}
	}
}

// parameter is one path or query field: its JSON field and how the generated code reads it.
type parameter struct {
	field description.Field
	plan  serverplan.Param
}

func fieldsOf(params []parameter) []description.Field {
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

// placeOfInputField is where the generated code reads an input field from.
func placeOfInputField(field *types.Var, tag string) inputField {
	if field.Embedded() {
		return embeddedInputField{}
	}
	if !field.Exported() {
		return unplacedField{}
	}
	return mo.Fold(fieldTag(tag).value("path"), func(name string) inputField { return pathField{name: name} }, func(error) inputField {
		return mo.Fold(fieldTag(tag).value("query"), func(name string) inputField { return queryField{name: name} }, func(error) inputField {
			if field.Name() == "Body" {
				return bodyField{}
			}
			return unplacedField{}
		})
	})
}

func (reader *projectReader) readInput(loaded *packages.Package, input types.Type, method description.Method, position token.Pos) mo.Option[inputShape] {
	named, isNamed := types.Unalias(input).(*types.Named)
	if !isNamed {
		reader.refuse(position, InputNotStruct{Input: types.TypeString(input, nil)})
		return mo.None[inputShape]()
	}
	fields, isStruct := named.Underlying().(*types.Struct)
	if !isStruct {
		reader.refuse(position, InputNotStruct{Input: types.TypeString(input, nil)})
		return mo.None[inputShape]()
	}
	return thenOption(reader.reachableName(named.Obj(), loaded.PkgPath, position), func(name serverplan.GoName) mo.Option[inputShape] {
		reader.read[packagePath(named.Obj())] = struct{}{}
		return reader.readInputFields(loaded, name, fields, method)
	})
}

func (reader *projectReader) readInputFields(loaded *packages.Package, name serverplan.GoName, fields *types.Struct, method description.Method) mo.Option[inputShape] {
	shape := inputShape{name: name, params: []parameter{}, query: []parameter{}, body: description.NoBody{}}
	complete := true
	collect := func(into *[]parameter) func(parameter) struct{} {
		return func(read parameter) struct{} {
			*into = append(*into, read)
			return struct{}{}
		}
	}
	refused := func(error) struct{} {
		complete = false
		return struct{}{}
	}
	for index := range fields.NumFields() {
		field := fields.Field(index)
		switch place := placeOfInputField(field, fields.Tag(index)).(type) {
		case pathField:
			mo.Fold(reader.readParameter(loaded, field, place.name), collect(&shape.params), refused)
		case queryField:
			mo.Fold(reader.readParameter(loaded, field, place.name), collect(&shape.query), refused)
		case bodyField:
			if _, isGet := method.(description.Get); isGet {
				reader.refuse(field.Pos(), BodyOnGet{})
				complete = false
				continue
			}
			mo.Fold(reader.schemaOf(field.Type(), field.Pos()), func(schema description.Schema) struct{} {
				shape.body = description.JSONBody{Schema: schema}
				return struct{}{}
			}, refused)
		case embeddedInputField:
			reader.refuse(field.Pos(), EmbeddedFieldUnsupported{Field: field.Name()})
			complete = false
		case unplacedField:
			reader.refuse(field.Pos(), InputFieldUnread{Field: field.Name()})
			complete = false
		case nil:
		}
	}
	if !complete {
		return mo.None[inputShape]()
	}
	return mo.Some(shape)
}

// readParameter reads a path or query field, whose text the generated code parses by its type.
func (reader *projectReader) readParameter(loaded *packages.Package, field *types.Var, name string) mo.Option[parameter] {
	unsupported := func() mo.Option[parameter] {
		reader.refuse(field.Pos(), ParameterTypeUnsupported{Field: field.Name(), Type: types.TypeString(field.Type(), nil)})
		return mo.None[parameter]()
	}
	basic, isBasic := field.Type().Underlying().(*types.Basic)
	if !isBasic {
		return unsupported()
	}
	goType := mo.Some(serverplan.GoName{Package: "", Name: basic.Name()})
	if named, isNamed := types.Unalias(field.Type()).(*types.Named); isNamed {
		goType = reader.reachableName(named.Obj(), loaded.PkgPath, field.Pos())
	}
	return thenOption(goType, func(goType serverplan.GoName) mo.Option[parameter] {
		return reader.readParameterSchema(field, name, basic, goType)
	})
}

func (reader *projectReader) readParameterSchema(field *types.Var, name string, basic *types.Basic, goType serverplan.GoName) mo.Option[parameter] {
	unsupported := func() mo.Option[parameter] {
		reader.refuse(field.Pos(), ParameterTypeUnsupported{Field: field.Name(), Type: types.TypeString(field.Type(), nil)})
		return mo.None[parameter]()
	}
	return thenOption(reader.schemaOf(field.Type(), field.Pos()), func(schema description.Schema) mo.Option[parameter] {
		return mo.Fold(textKind(basic, schema), func(kind serverplan.TextKind) mo.Option[parameter] {
			return mo.Some(parameter{
				field: description.Field{Name: name, Schema: schema},
				plan:  serverplan.Param{Name: name, GoField: field.Name(), Text: kind, GoType: goType},
			})
		}, func(error) mo.Option[parameter] { return unsupported() })
	})
}

// textKind is how the generated code parses a parameter of a basic type and its schema.
func textKind(basic *types.Basic, schema description.Schema) mo.Option[serverplan.TextKind] {
	if literals, isLiterals := schema.(description.Literals); isLiterals {
		return mo.Some[serverplan.TextKind](serverplan.TextLiteral{Values: literals.Values})
	}
	switch basic.Kind() {
	case types.String:
		return mo.Some[serverplan.TextKind](serverplan.TextAsIs{})
	case types.Bool:
		return mo.Some[serverplan.TextKind](serverplan.TextTruth{})
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64:
		return mo.Some[serverplan.TextKind](serverplan.TextSigned{Bits: bitsOf(basic.Kind())})
	case types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		return mo.Some[serverplan.TextKind](serverplan.TextUnsigned{Bits: bitsOf(basic.Kind())})
	case types.Invalid, types.Uintptr, types.Float32, types.Float64, types.Complex64, types.Complex128, types.UnsafePointer,
		types.UntypedBool, types.UntypedInt, types.UntypedRune, types.UntypedFloat, types.UntypedComplex, types.UntypedString, types.UntypedNil:
	}
	return mo.None[serverplan.TextKind]()
}

// bitsOf is the size strconv parses a whole number kind to, 0 meaning an int's size.
func bitsOf(kind types.BasicKind) int {
	for _, sized := range []struct {
		kind types.BasicKind
		bits int
	}{
		{kind: types.Int8, bits: 8},
		{kind: types.Int16, bits: 16},
		{kind: types.Int32, bits: 32},
		{kind: types.Int64, bits: 64},
		{kind: types.Uint8, bits: 8},
		{kind: types.Uint16, bits: 16},
		{kind: types.Uint32, bits: 32},
		{kind: types.Uint64, bits: 64},
	} {
		if sized.kind == kind {
			return sized.bits
		}
	}
	return 0
}
