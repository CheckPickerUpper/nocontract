package reading

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// ValueServedNotAPI is a call that serves HTTP with a handler whose type is not the generated API,
// so it could answer a route the client and the OpenAPI file do not list.
//
//ergo:code NC-1072
//ergo:message {Function} serves a {Type}; serve the generated API, which answers only the declared routes.
type ValueServedNotAPI struct {
	Function string
	Type     string
}

// ServingFunctionNotCalled is a function that serves HTTP used other than by calling it, so what
// it serves cannot be read.
//
//ergo:code NC-1073
//ergo:message {Function} is used without calling it, so what it serves cannot be read; call it with the generated API.
type ServingFunctionNotCalled struct{ Function string }

// ServerHandlerNotAPI is an http.Server written with a Handler whose type is not the generated API.
//
//ergo:code NC-1074
//ergo:message The http.Server is written with a {Type} as its Handler; give it the generated API.
type ServerHandlerNotAPI struct{ Type string }

// ServerHandlerMissing is an http.Server written without a Handler, which serves
// http.DefaultServeMux and whatever any package registered on it.
//
//ergo:code NC-1075
//ergo:message The http.Server is written without a Handler, so it serves http.DefaultServeMux; give it the generated API.
type ServerHandlerMissing struct{}

// ServerHandlerAssigned is an http.Server's Handler set to a value whose type is not the generated
// API.
//
//ergo:code NC-1076
//ergo:message An http.Server's Handler is set to a {Type}; set it to the generated API.
type ServerHandlerAssigned struct{ Type string }

// HandlerFuncUsed is http.HandlerFunc named in a file the project wrote, which makes a function a
// handler outside the generated API.
//
//ergo:code NC-1077
//ergo:message http.HandlerFunc makes a handler outside the generated API; run code around every request with a nocontract.Layer.
type HandlerFuncUsed struct{}

// ServeHTTPDeclared is a ServeHTTP method declared on a project type, which makes the type a
// handler outside the generated API.
//
//ergo:code NC-1078
//ergo:message {Type} declares ServeHTTP, which makes it a handler outside the generated API; run code around every request with a nocontract.Layer.
type ServeHTTPDeclared struct{ Type string }

// HandlerRegistered is a handler registered on an http.ServeMux, which answers a route outside the
// generated API.
//
//ergo:code NC-1079
//ergo:message {Function} registers a handler outside the generated API; declare the route with //nocontract:route.
type HandlerRegistered struct{ Function string }

func (ValueServedNotAPI) isProblem()        {}
func (ServingFunctionNotCalled) isProblem() {}
func (ServerHandlerNotAPI) isProblem()      {}
func (ServerHandlerMissing) isProblem()     {}
func (ServerHandlerAssigned) isProblem()    {}
func (HandlerFuncUsed) isProblem()          {}
func (ServeHTTPDeclared) isProblem()        {}
func (HandlerRegistered) isProblem()        {}

// ServingRequest is what the serving judge reads: every package the project file lists, loaded
// with its tests and with each file generate writes as generate writes it, and the server folder.
type ServingRequest struct {
	Packages []*packages.Package
	Server   string
}

// JudgeServing is a refusal for each place a file the project wrote serves HTTP with anything but
// the generated API, or makes a handler of its own. A file built into several of the packages
// loaded, such as a package and the same package with its tests, is judged once.
func JudgeServing(request ServingRequest) []assembling.Refusal {
	refusals := []assembling.Refusal{}
	judged := map[string]struct{}{}
	loaded := slices.SortedFunc(slices.Values(request.Packages), func(left, right *packages.Package) int { return cmp.Compare(left.ID, right.ID) })
	for _, each := range loaded {
		for _, file := range each.Syntax {
			name := each.Fset.Position(file.Pos()).Filename
			if _, isJudged := judged[name]; isJudged || filepath.Dir(name) != each.Dir {
				// A file outside the package's folder is the main the go command writes to run tests.
				continue
			}
			judged[name] = struct{}{}
			switch serverplan.SourceFileAt(name).(type) {
			case serverplan.WrittenFile:
				judge := servingJudge{
					files: each.Fset, info: each.TypesInfo, apiFile: filepath.Join(request.Server, serverplan.ServerFileName),
					called: map[*ast.Ident]struct{}{}, refusals: []assembling.Refusal{},
				}
				ast.Inspect(file, judge.judge)
				refusals = append(refusals, judge.refusals...)
			case serverplan.GeneratedFile, nil:
			}
		}
	}
	return refusals
}

// servingJudge judges one file: the positions and types it was loaded with, the file declaring the
// generated API, each serving function's name it saw called, and what it refused.
type servingJudge struct {
	files    *token.FileSet
	info     *types.Info
	apiFile  string
	called   map[*ast.Ident]struct{}
	refusals []assembling.Refusal
}

// servingFunction is a function that serves HTTP with a handler, and which of its arguments the
// handler is.
type servingFunction struct {
	name    string
	handler int
}

// servingFunctions is every function of net/http and net/http/httptest that serves a handler it
// is handed.
func servingFunctions() []servingFunction {
	return []servingFunction{
		{name: "net/http.ListenAndServe", handler: 1},
		{name: "net/http.ListenAndServeTLS", handler: 3},
		{name: "net/http.Serve", handler: 1},
		{name: "net/http.ServeTLS", handler: 1},
		{name: "net/http/httptest.NewServer", handler: 0},
		{name: "net/http/httptest.NewTLSServer", handler: 0},
		{name: "net/http/httptest.NewUnstartedServer", handler: 0},
	}
}

// registeringFunctions is every function of net/http that registers a handler on a ServeMux.
func registeringFunctions() []string {
	return []string{"net/http.Handle", "net/http.HandleFunc", "(*net/http.ServeMux).Handle", "(*net/http.ServeMux).HandleFunc"}
}

// servedType is what a value handed to a server is.
//
//sumtype:decl
type servedType interface{ isServedType() }

// servesAPI is a value of the generated API.
//
//ergo:not-an-error
type servesAPI struct{}

// servesOther is a value of any other type, written as Go writes it.
//
//ergo:not-an-error
type servesOther struct{ written string }

func (servesAPI) isServedType()   {}
func (servesOther) isServedType() {}

// servedTypeOf is what a value of the type is: the generated API is the type named API that the
// generated server file declares.
func (judge *servingJudge) servedTypeOf(served types.Type) servedType {
	if named, isNamed := types.Unalias(served).(*types.Named); isNamed && named.Obj().Name() == serverplan.ServerType &&
		judge.files.Position(named.Obj().Pos()).Filename == judge.apiFile {
		return servesAPI{}
	}
	return servesOther{written: types.TypeString(served, nil)}
}

// refuse records a refusal of what is written at pos.
func (judge *servingJudge) refuse(pos token.Pos, problem Problem) {
	judge.refusals = append(judge.refusals, assembling.Refusal{Position: placedAt(judge.files.Position(pos)), Problem: problem})
}

// judge refuses the node when it serves or makes a handler, and goes on into its children.
func (judge *servingJudge) judge(node ast.Node) bool {
	switch written := node.(type) {
	case *ast.CallExpr:
		judge.judgeCall(written)
	case *ast.Ident:
		judge.judgeName(written)
	case *ast.FuncDecl:
		judge.judgeMethod(written)
	case *ast.CompositeLit:
		judge.judgeServerLiteral(written)
	case *ast.AssignStmt:
		judge.judgeHandlerAssignment(written)
	case nil:
	}
	return true
}

// calledName is the name a call calls, written as a name or as a selector.
func calledName(call *ast.CallExpr) []*ast.Ident {
	switch callee := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return []*ast.Ident{callee}
	case *ast.SelectorExpr:
		return []*ast.Ident{callee.Sel}
	case nil:
	}
	return []*ast.Ident{}
}

// judgeCall refuses a call to a serving function whose handler is not the generated API.
func (judge *servingJudge) judgeCall(call *ast.CallExpr) {
	for _, name := range calledName(call) {
		function, isFunction := judge.info.Uses[name].(*types.Func)
		if !isFunction {
			continue
		}
		for _, serving := range servingFunctions() {
			if serving.name != function.FullName() || len(call.Args) <= serving.handler {
				continue
			}
			judge.called[name] = struct{}{}
			handler := call.Args[serving.handler]
			switch served := judge.servedTypeOf(judge.info.TypeOf(handler)).(type) {
			case servesOther:
				judge.refuse(handler.Pos(), ValueServedNotAPI{Function: serving.name, Type: served.written})
			case servesAPI, nil:
			}
		}
	}
}

// judgeName refuses http.HandlerFunc, a registering function, and a serving function used without
// calling it.
func (judge *servingJudge) judgeName(name *ast.Ident) {
	switch used := judge.info.Uses[name].(type) {
	case *types.TypeName:
		if types.TypeString(used.Type(), nil) == "net/http.HandlerFunc" {
			judge.refuse(name.Pos(), HandlerFuncUsed{})
		}
	case *types.Func:
		if slices.Contains(registeringFunctions(), used.FullName()) {
			judge.refuse(name.Pos(), HandlerRegistered{Function: used.FullName()})
		}
		for _, serving := range servingFunctions() {
			if _, isCalled := judge.called[name]; serving.name == used.FullName() && !isCalled {
				judge.refuse(name.Pos(), ServingFunctionNotCalled{Function: serving.name})
			}
		}
	case nil:
	}
}

// judgeMethod refuses a ServeHTTP method that makes its receiver an http.Handler.
func (judge *servingJudge) judgeMethod(declared *ast.FuncDecl) {
	method, isMethod := judge.info.Defs[declared.Name].(*types.Func)
	if !isMethod || declared.Recv.NumFields() == 0 || method.Name() != "ServeHTTP" {
		return
	}
	// A method's signature is written as a function's, without its receiver.
	if types.TypeString(method.Type(), nil) != "func(net/http.ResponseWriter, *net/http.Request)" {
		return
	}
	judge.refuse(declared.Name.Pos(), ServeHTTPDeclared{Type: types.TypeString(method.Signature().Recv().Type(), nil)})
}

// judgeServerLiteral refuses an http.Server written without a Handler, or with one that is not the
// generated API.
func (judge *servingJudge) judgeServerLiteral(literal *ast.CompositeLit) {
	if types.TypeString(judge.info.TypeOf(literal), nil) != "net/http.Server" {
		return
	}
	for _, element := range literal.Elts {
		field, isField := element.(*ast.KeyValueExpr)
		if !isField {
			continue
		}
		if key, isKey := field.Key.(*ast.Ident); isKey && key.Name == "Handler" {
			switch served := judge.servedTypeOf(judge.info.TypeOf(field.Value)).(type) {
			case servesOther:
				judge.refuse(field.Value.Pos(), ServerHandlerNotAPI{Type: served.written})
			case servesAPI, nil:
			}
			return
		}
	}
	judge.refuse(literal.Pos(), ServerHandlerMissing{})
}

// judgeHandlerAssignment refuses setting an http.Server's Handler to anything but the generated
// API.
func (judge *servingJudge) judgeHandlerAssignment(assignment *ast.AssignStmt) {
	for i, target := range assignment.Lhs {
		selector, isSelector := target.(*ast.SelectorExpr)
		if !isSelector {
			continue
		}
		selection, isSelection := judge.info.Selections[selector]
		if !isSelection || selection.Kind() != types.FieldVal || selection.Obj().Name() != "Handler" || selection.Obj().Pkg().Path() != "net/http" {
			continue
		}
		switch served := judge.servedTypeOf(assignedType(judge.info, assignment, i)).(type) {
		case servesOther:
			judge.refuse(target.Pos(), ServerHandlerAssigned{Type: served.written})
		case servesAPI, nil:
		}
	}
}

// assignedType is the type of the value an assignment gives its target at index: the value
// written for it, or the result at index of the one call written for every target.
func assignedType(info *types.Info, assignment *ast.AssignStmt, index int) types.Type {
	if len(assignment.Rhs) == len(assignment.Lhs) {
		return info.TypeOf(assignment.Rhs[index])
	}
	if results, isTuple := info.TypeOf(assignment.Rhs[0]).(*types.Tuple); isTuple {
		return results.At(index).Type()
	}
	return info.TypeOf(assignment.Rhs[0])
}
