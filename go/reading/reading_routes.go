// Package reading reads a Go project's //nocontract:route handlers, their inputs and the members
// of their outcomes out of Go's type checker, as the language-neutral description and the Go
// server plan, and refuses every declaration the generated server and its client cannot hold.
package reading

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"

	"github.com/CheckPickerUpper/ErGo/declarations"
	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// NoContractPath is the import path of the Go package the generated code is built from, whose own
// failures every route can answer.
const NoContractPath = "github.com/CheckPickerUpper/nocontract/go"

// GeneratedFileName is the Go file written into each package that declares routes.
const GeneratedFileName = "nocontract_routes.go"

// Request is what the generator reads: the loaded packages, the project's own and nocontract's,
// each with its syntax, and the name of the Effect API.
type Request struct {
	Packages []*packages.Package
	APIName  string
}

// Reading is what reading a project's routes ended as.
//
//sumtype:decl
type Reading interface{ isReading() }

// RoutesRead is every route, read without a refusal, as the description and the Go server plan,
// and the import path of every package read.
//
//ergo:not-an-error
type RoutesRead struct {
	API  description.API
	Plan serverplan.Plan
	Read []string
}

// RoutesRefused is every declaration that could not be read, in source order, and the import
// path of every package read.
//
//ergo:code NC-1031
type RoutesRefused struct {
	Refusals []Refusal
	Read     []string
}

func (RoutesRead) isReading()    {}
func (RoutesRefused) isReading() {}

// routeMethods is every method a route directive may name.
func routeMethods() []string {
	return []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
}

// reservedNames are the names the TypeScript file imports, which no declaration may take.
func reservedNames() []string {
	return []string{"Schema", "HttpApi", "HttpApiEndpoint", "HttpApiGroup", "HttpApiSchema"}
}

// ReadRoutes reads every route the project's packages declare.
func ReadRoutes(request Request) Reading {
	reader := newProjectReader(request)
	shared := description.SharedFailures{
		RequestRefused:   reader.readSharedFailure("RequestRefused"),
		AnswerNotWritten: reader.readSharedFailure("AnswerNotWritten"),
	}
	for _, loaded := range reader.projectPackages() {
		for _, file := range loaded.Syntax {
			if filepath.Base(loaded.Fset.File(file.Pos()).Name()) == GeneratedFileName {
				continue
			}
			for _, declaration := range file.Decls {
				if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
					reader.readHandler(loaded, function)
				}
			}
		}
	}
	read := reader.readPackages()
	if len(reader.refusals) > 0 {
		refusals := slices.Clone(reader.refusals)
		slices.SortStableFunc(refusals, func(left, right Refusal) int {
			return cmp.Or(cmp.Compare(left.Position.Filename, right.Position.Filename), cmp.Compare(left.Position.Line, right.Position.Line))
		})
		return RoutesRefused{Refusals: refusals, Read: read}
	}
	return reader.routesRead(shared, read)
}

// typeKey names a declared type by its package's import path and its name, so types read through
// different packages' imports are one type.
type typeKey struct {
	path string
	name string
}

func keyOf(object *types.TypeName) typeKey {
	return typeKey{path: packagePath(object), name: object.Name()}
}

// packagePath is the import path of the package an object is declared in, empty for a
// predeclared one.
func packagePath(object types.Object) string {
	return mo.Fold(mo.EmptyableToOption(object.Pkg()), func(owner *types.Package) string { return owner.Path() }, func(error) string { return "" })
}

// declaredType is a type declaration the project's syntax holds.
type declaredType struct {
	spec    *ast.TypeSpec
	loaded  *packages.Package
	object  *types.TypeName
	comment *ast.CommentGroup
}

// projectReader holds what reading has found so far and every refusal.
type projectReader struct {
	request   Request
	files     *token.FileSet
	declared  map[typeKey]declaredType
	names     map[string]token.Position
	structs   map[typeKey]structState
	structOut []description.NamedStruct
	members   map[typeKey]memberReading
	memberOut []description.Member
	outcomes  map[typeKey]outcomeReading
	patterns  map[string]token.Position
	groups    map[string]*groupBuild
	read      map[string]struct{}
	refusals  []Refusal
}

// groupBuild is the endpoints of one package and the routes its generated code serves, as they
// are read.
type groupBuild struct {
	group description.Group
	plan  serverplan.Package
}

func newProjectReader(request Request) *projectReader {
	reader := &projectReader{
		request: request, files: token.NewFileSet(), declared: map[typeKey]declaredType{}, names: map[string]token.Position{},
		structs: map[typeKey]structState{}, structOut: []description.NamedStruct{}, members: map[typeKey]memberReading{},
		memberOut: []description.Member{}, outcomes: map[typeKey]outcomeReading{}, patterns: map[string]token.Position{},
		groups: map[string]*groupBuild{}, read: map[string]struct{}{}, refusals: []Refusal{},
	}
	for _, loaded := range request.Packages {
		reader.files = loaded.Fset
		reader.indexDeclarations(loaded)
	}
	for _, name := range append(reservedNames(), request.APIName) {
		reader.names[name] = token.Position{Filename: "", Offset: 0, Line: 0, Column: 0}
	}
	return reader
}

func (reader *projectReader) indexDeclarations(loaded *packages.Package) {
	for _, file := range loaded.Syntax {
		for _, declaration := range file.Decls {
			group, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral || group.Tok != token.TYPE {
				continue
			}
			for _, spec := range group.Specs {
				typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
				if !isTypeSpec {
					continue
				}
				if object, isTypeName := loaded.TypesInfo.Defs[typeSpec.Name].(*types.TypeName); isTypeName {
					comment := mo.Fold(mo.EmptyableToOption(declarations.TypeComment(typeSpec, group)), func(found *ast.CommentGroup) *ast.CommentGroup { return found },
						func(error) *ast.CommentGroup { return &ast.CommentGroup{List: []*ast.Comment{}} })
					reader.declared[keyOf(object)] = declaredType{spec: typeSpec, loaded: loaded, object: object, comment: comment}
				}
			}
		}
	}
}

// projectPackages is every loaded package other than nocontract, in import path order.
func (reader *projectReader) projectPackages() []*packages.Package {
	project := []*packages.Package{}
	for _, loaded := range reader.request.Packages {
		if loaded.PkgPath != NoContractPath {
			project = append(project, loaded)
		}
	}
	slices.SortFunc(project, func(left, right *packages.Package) int { return cmp.Compare(left.PkgPath, right.PkgPath) })
	return project
}

// readPackages is the import path of every package whose declarations were read, sorted.
func (reader *projectReader) readPackages() []string {
	read := []string{}
	for path := range reader.read {
		read = append(read, path)
	}
	slices.Sort(read)
	return read
}

func (reader *projectReader) refuse(position token.Pos, problem Problem) {
	reader.refusals = append(reader.refusals, Refusal{Position: reader.files.Position(position), Problem: problem})
}

// reserve takes name for the declaration at position, or refuses it when another holds it.
func (reader *projectReader) reserve(name string, position token.Pos) {
	if first, taken := reader.names[name]; taken {
		reader.refuse(position, NameRepeated{Name: name, First: first})
		return
	}
	reader.names[name] = reader.files.Position(position)
}

// readSharedFailure reads one of nocontract's own failures as a member every route can answer.
func (reader *projectReader) readSharedFailure(name string) string {
	declared, isDeclared := reader.declared[typeKey{path: NoContractPath, name: name}]
	if !isDeclared {
		reader.refuse(token.NoPos, DeclarationOutsideProject{Type: NoContractPath + "." + name})
		return name
	}
	reader.readMember(declared)
	return name
}

func (reader *projectReader) routesRead(shared description.SharedFailures, read []string) RoutesRead {
	groups, packages := []description.Group{}, []serverplan.Package{}
	for _, build := range reader.groups {
		group := build.group
		group.Endpoints = slices.Clone(group.Endpoints)
		slices.SortFunc(group.Endpoints, func(left, right description.Endpoint) int { return cmp.Compare(left.Name, right.Name) })
		groups = append(groups, group)
		served := build.plan
		served.Routes = slices.Clone(served.Routes)
		slices.SortFunc(served.Routes, func(left, right serverplan.Route) int { return cmp.Compare(left.Handler.Name, right.Handler.Name) })
		packages = append(packages, served)
	}
	slices.SortFunc(groups, func(left, right description.Group) int { return cmp.Compare(left.Name, right.Name) })
	slices.SortFunc(packages, func(left, right serverplan.Package) int { return cmp.Compare(left.Path, right.Path) })
	return RoutesRead{
		API:  description.API{Name: reader.request.APIName, Groups: groups, Members: reader.memberOut, Structs: reader.structOut, Shared: shared},
		Plan: serverplan.Plan{Packages: packages},
		Read: read,
	}
}
