package reading

import (
	"go/ast"
	"go/types"
)

// ZeroServerMade is an http.Server made at its zero value, whose nil Handler serves
// http.DefaultServeMux and whatever any imported package registered on it, and how it is made.
//
//ergo:code NC-1080
//ergo:message An http.Server is made at its zero value, which serves http.DefaultServeMux: {Way, select, ServerDeclaredWithoutValue {it is declared with no value} ServerAllocatedWithNew {new allocates it} ServerHeldByValue {a struct field, or an array, slice, map or channel element, holds it by value} ServerTypeDefined {a type is defined as it} ServerTypeArgument {it is a type argument} ServerNamedResult {it is a named result}}; write an http.Server literal with the generated API as its Handler, and hold it by pointer.
type ZeroServerMade struct{ Way ZeroServerWay }

func (ZeroServerMade) isProblem() {}

// ZeroServerWay is how a project file makes an http.Server at its zero value.
//
//sumtype:decl
type ZeroServerWay interface{ isZeroServerWay() }

// ServerDeclaredWithoutValue is a variable of type http.Server declared with no value.
//
//ergo:not-an-error
type ServerDeclaredWithoutValue struct{}

// ServerAllocatedWithNew is new(http.Server).
//
//ergo:not-an-error
type ServerAllocatedWithNew struct{}

// ServerHeldByValue is a struct field, or an array, slice, map or channel element, of type
// http.Server: a literal or make that leaves it out makes it zero.
//
//ergo:not-an-error
type ServerHeldByValue struct{}

// ServerTypeDefined is a type defined as http.Server, whose zero value is a server.
//
//ergo:not-an-error
type ServerTypeDefined struct{}

// ServerTypeArgument is http.Server as a type argument, which generic code can make at its zero
// value.
//
//ergo:not-an-error
type ServerTypeArgument struct{}

// ServerNamedResult is a named result of type http.Server, which starts at its zero value.
//
//ergo:not-an-error
type ServerNamedResult struct{}

func (ServerDeclaredWithoutValue) isZeroServerWay() {}
func (ServerAllocatedWithNew) isZeroServerWay()     {}
func (ServerHeldByValue) isZeroServerWay()          {}
func (ServerTypeDefined) isZeroServerWay()          {}
func (ServerTypeArgument) isZeroServerWay()         {}
func (ServerNamedResult) isZeroServerWay()          {}

// serverTypeUse is what the code enclosing an http.Server type does with it.
//
//sumtype:decl
type serverTypeUse interface{ isServerTypeUse() }

// serverMadeZero is a use that makes a server at its zero value, and how.
//
//ergo:not-an-error
type serverMadeZero struct{ way ZeroServerWay }

// serverGivenValue is a use whose server has a value written for it or copied from another: a
// literal, a pointer, a variable with a value, a conversion, a type assertion or switch, an alias,
// or a parameter or unnamed result.
//
//ergo:not-an-error
type serverGivenValue struct{}

func (serverMadeZero) isServerTypeUse()   {}
func (serverGivenValue) isServerTypeUse() {}

// judgeServerType refuses an http.Server type written where it makes a server at its zero value.
func (judge *servingJudge) judgeServerType(written ast.Expr) {
	typed, isRecorded := judge.info.Types[written]
	if !isRecorded || !typed.IsType() || types.TypeString(typed.Type, nil) != "net/http.Server" {
		return
	}
	// A parenthesized type is judged once, at the type inside the parentheses.
	if _, isParenthesized := written.(*ast.ParenExpr); isParenthesized {
		return
	}
	switch use := judge.serverTypeUseAt().(type) {
	case serverMadeZero:
		judge.refuse(written.Pos(), ZeroServerMade{Way: use.way})
	case serverGivenValue, nil:
	}
}

// serverTypeUseAt is what the code enclosing the http.Server type being judged does with it,
// looking past parentheses.
func (judge *servingJudge) serverTypeUseAt() serverTypeUse {
	for depth := len(judge.parents) - 1; depth >= 0; depth-- {
		switch parent := judge.parents[depth].(type) {
		case *ast.ParenExpr:
			continue
		case *ast.ValueSpec:
			if len(parent.Values) == 0 {
				return serverMadeZero{way: ServerDeclaredWithoutValue{}}
			}
		case *ast.CallExpr:
			if builtin, isBuiltin := judge.info.Uses[calledBuiltin(parent)].(*types.Builtin); isBuiltin && builtin.Name() == "new" {
				return serverMadeZero{way: ServerAllocatedWithNew{}}
			}
		case *ast.ArrayType, *ast.MapType, *ast.ChanType:
			return serverMadeZero{way: ServerHeldByValue{}}
		case *ast.TypeSpec:
			if !parent.Assign.IsValid() {
				return serverMadeZero{way: ServerTypeDefined{}}
			}
		case *ast.IndexExpr, *ast.IndexListExpr:
			return serverMadeZero{way: ServerTypeArgument{}}
		case *ast.Field:
			return judge.fieldUse(parent, depth)
		case nil:
		}
		return serverGivenValue{}
	}
	return serverGivenValue{}
}

// calledBuiltin is the name a call calls when it is written as a bare name, as a builtin is.
func calledBuiltin(call *ast.CallExpr) *ast.Ident {
	if name, isName := ast.Unparen(call.Fun).(*ast.Ident); isName {
		return name
	}
	return ast.NewIdent("")
}

// fieldUse is what a field of type http.Server does with it: a struct field holds a server by
// value, a named result starts at its zero value, and a parameter or unnamed result is handed one.
func (judge *servingJudge) fieldUse(field *ast.Field, depth int) serverTypeUse {
	if depth < 2 {
		return serverGivenValue{}
	}
	list, isList := judge.parents[depth-1].(*ast.FieldList)
	if !isList {
		return serverGivenValue{}
	}
	switch owner := judge.parents[depth-2].(type) {
	case *ast.StructType:
		return serverMadeZero{way: ServerHeldByValue{}}
	case *ast.FuncType:
		if owner.Results == list && len(field.Names) > 0 {
			return serverMadeZero{way: ServerNamedResult{}}
		}
	case nil:
	}
	return serverGivenValue{}
}
