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
	method string
	path   string
}

// readRouteLine reads "METHOD /path".
func readRouteLine(text string) mo.Option[routeLine] {
	words := strings.Fields(text)
	if len(words) != 2 || !slices.Contains(routeMethods(), words[0]) || !strings.HasPrefix(words[1], "/") {
		return mo.None[routeLine]()
	}
	return mo.Some(routeLine{method: words[0], path: words[1]})
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

// handlerTypes is a handler's input and outcome types, read from its signature.
//
//ergo:not-an-error
type handlerTypes struct {
	input   types.Type
	outcome types.Type
}

func (handlerTypes) isSignatureReading()                {}
func (HandlerSignatureUnsupported) isSignatureReading() {}

// signatureOf is the input and outcome of a func(context.Context, Input) Outcome, or why the
// handler is not one.
func signatureOf(function *ast.FuncDecl, object *types.Func) signatureReading {
	signature, isSignature := object.Type().(*types.Signature)
	refused := func(reason string) signatureReading {
		return HandlerSignatureUnsupported{Handler: function.Name.Name, Reason: reason}
	}
	switch {
	case !isSignature:
		return refused("it is not a function")
	case function.Recv.NumFields() > 0:
		return refused("it is a method; declare the route on a function")
	case signature.TypeParams().Len() > 0:
		return refused("it has type parameters")
	case signature.Params().Len() != 2 || signature.Results().Len() != 1:
		return refused("it does not take a context and an input and return one outcome")
	}
	context, isNamed := types.Unalias(signature.Params().At(0).Type()).(*types.Named)
	if !isNamed || packagePath(context.Obj()) != "context" || context.Obj().Name() != "Context" {
		return refused("its first parameter is not a context.Context")
	}
	return handlerTypes{input: signature.Params().At(1).Type(), outcome: signature.Results().At(0).Type()}
}

func emptyComment() *ast.CommentGroup { return &ast.CommentGroup{List: []*ast.Comment{}} }

func (reader *projectReader) readHandler(loaded *packages.Package, function *ast.FuncDecl) {
	doc := mo.Fold(mo.EmptyableToOption(function.Doc), func(found *ast.CommentGroup) *ast.CommentGroup { return found },
		func(error) *ast.CommentGroup { return emptyComment() })
	routes := named(directivesIn(doc), routeDirective)
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
		switch read := signatureOf(function, object).(type) {
		case handlerTypes:
			reader.readRoute(loaded, function, route, read)
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

func (reader *projectReader) readRoute(loaded *packages.Package, function *ast.FuncDecl, route routeLine, handler handlerTypes) {
	position := function.Name.Pos()
	pattern := route.method + " " + route.path
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
			reader.addRoute(loaded, function, description.Endpoint{
				Name: endpointName(function.Name.Name), Method: route.method, Path: route.path,
				Params: fieldsOf(read.params), Query: fieldsOf(read.query), Body: read.body, Members: members.names,
			}, serverplan.Route{
				Method: route.method, Path: route.path, Handler: serverplan.GoName{Package: loaded.PkgPath, Name: function.Name.Name},
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

func (reader *projectReader) addRoute(loaded *packages.Package, function *ast.FuncDecl, endpoint description.Endpoint, route serverplan.Route) {
	build, known := reader.groups[loaded.PkgPath]
	if !known {
		reader.reserve(description.GroupClassName(loaded.Name), function.Name.Pos())
		build = &groupBuild{
			group: description.Group{Name: loaded.Name, Endpoints: []description.Endpoint{}},
			plan: serverplan.Package{
				Name: loaded.Name, Path: loaded.PkgPath, Directory: filepath.Dir(reader.files.Position(function.Pos()).Filename),
				Routes: []serverplan.Route{},
			},
		}
		reader.groups[loaded.PkgPath] = build
	}
	build.group.Endpoints = append(build.group.Endpoints, endpoint)
	build.plan.Routes = append(build.plan.Routes, route)
}

// reachable refuses a type the route's generated code in package from cannot name.
func (reader *projectReader) reachable(object *types.TypeName, from string, position token.Pos) bool {
	if object.Exported() || packagePath(object) == from || packagePath(object) == "" {
		return true
	}
	reader.refuse(position, TypeNotReachable{Type: packagePath(object) + "." + object.Name()})
	return false
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

// tagValue is the value of the struct tag key, when the tag carries it.
func tagValue(tag, key string) mo.Option[string] {
	return mo.Fold(mo.TupleToResult(structtag.Parse(tag)), func(tags *structtag.Tags) mo.Option[string] {
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
	return mo.Fold(tagValue(tag, "path"), func(name string) inputField { return pathField{name: name} }, func(error) inputField {
		return mo.Fold(tagValue(tag, "query"), func(name string) inputField { return queryField{name: name} }, func(error) inputField {
			if field.Name() == "Body" {
				return bodyField{}
			}
			return unplacedField{}
		})
	})
}

func (reader *projectReader) readInput(loaded *packages.Package, input types.Type, method string, position token.Pos) mo.Option[inputShape] {
	named, isNamed := types.Unalias(input).(*types.Named)
	if !isNamed {
		reader.refuse(position, InputNotStruct{Input: types.TypeString(input, nil)})
		return mo.None[inputShape]()
	}
	fields, isStruct := named.Underlying().(*types.Struct)
	if !isStruct || !reader.reachable(named.Obj(), loaded.PkgPath, position) {
		reader.refuse(position, InputNotStruct{Input: types.TypeString(input, nil)})
		return mo.None[inputShape]()
	}
	reader.read[packagePath(named.Obj())] = struct{}{}
	shape := inputShape{name: goName(named.Obj()), params: []parameter{}, query: []parameter{}, body: description.NoBody{}}
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
			if method == "GET" {
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
	goType := serverplan.GoName{Package: "", Name: basic.Name()}
	if named, isNamed := types.Unalias(field.Type()).(*types.Named); isNamed {
		if !reader.reachable(named.Obj(), loaded.PkgPath, field.Pos()) {
			return mo.None[parameter]()
		}
		goType = goName(named.Obj())
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
