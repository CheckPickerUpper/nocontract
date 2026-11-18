// Package reading reads a Go project's //nocontract:route handlers, their inputs and the members
// of their outcomes out of Go's type checker, as the API they declare and the Go server plan, and
// refuses every Go declaration the generated server cannot hold. The checks that span routes, and
// hold for every backend language, run in package assembling over the API declared here.
//
// Every reading step is a function returning its value and everything it found: refusals, the
// members and named structs it read, and the packages whose declarations it read.
package reading

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"github.com/CheckPickerUpper/ErGo/declarations"
	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// Request is what the generator reads: the loaded packages, the project's own and nocontract's,
// each with its syntax, the sizes Go type-checked them with, which give int, uint and uintptr their
// width, the API's name and where it is given, and the absolute path of the folder the server
// package is written into. The project's packages are loaded with every Go file NoContract wrote
// left out of the build, so each file they hold is one the project wrote.
type Request struct {
	Packages []*packages.Package
	Sizes    types.Sizes
	API      assembling.NamedAPI
	Server   string
}

// Reading is what reading a project's routes found: the import path of every package read,
// sorted, and the routes read whole or refused.
type Reading struct {
	Packages []string
	Routes   RoutesReading
}

// RoutesReading is the project's routes read whole, or the declarations refused.
//
//sumtype:decl
type RoutesReading interface{ isRoutesReading() }

// RoutesRead is every route read whole: the API the routes declare, which assembling checks across
// routes, and the Go server plan that serves them.
//
//ergo:not-an-error
type RoutesRead struct {
	Declared assembling.Declared
	Plan     serverplan.Plan
}

// RoutesRefused is every Go declaration refused, and the API the declarations read whole declare,
// which assembling still checks across routes.
//
//ergo:not-an-error
type RoutesRefused struct {
	Declared assembling.Declared
	Refusals []assembling.Refusal
}

func (RoutesRead) isRoutesReading()    {}
func (RoutesRefused) isRoutesReading() {}

// ReadRoutes reads every route the project's packages declare. The packages read include
// nocontract's own, whose declarations the routes' generated code uses.
func ReadRoutes(request Request) Reading {
	index := indexed(request)
	guards := readGuards(index)
	return assembledPlan(index, guards.found.with(packageRead(serverplan.NoContractPath)), guards, readRoutes(index, guards))
}

// typeKey names a declared type by its package's import path and its name, so types read through
// different packages' imports are one type.
type typeKey struct {
	path string
	name string
}

// typeKeyFor is the key of a declared type.
func typeKeyFor(object *types.TypeName) typeKey {
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

// ownFile is a Go file the project wrote, and its package.
type ownFile struct {
	loaded *packages.Package
	file   *ast.File
}

// projectIndex is what every reading step reads from: the request, the file set positions are
// read through, every type declaration of the loaded syntax, the project's own packages in import
// path order, and the files of them the project wrote and the types those declare.
type projectIndex struct {
	request  Request
	files    *token.FileSet
	declared map[typeKey]declaredType
	project  []*packages.Package
	own      []ownFile
	ownTypes []declaredType
}

// indexed is the loaded packages indexed once.
func indexed(request Request) projectIndex {
	files := token.NewFileSet()
	declared := map[typeKey]declaredType{}
	project := []*packages.Package{}
	for _, loaded := range request.Packages {
		files = loaded.Fset
		for _, file := range loaded.Syntax {
			for _, entry := range typeDeclarations(loaded, file) {
				declared[typeKeyFor(entry.object)] = entry
			}
		}
		if loaded.PkgPath != serverplan.NoContractPath {
			project = append(project, loaded)
		}
	}
	slices.SortFunc(project, func(left, right *packages.Package) int { return cmp.Compare(left.PkgPath, right.PkgPath) })
	own, ownTypes := []ownFile{}, []declaredType{}
	for _, loaded := range project {
		for _, file := range loaded.Syntax {
			own = append(own, ownFile{loaded: loaded, file: file})
			ownTypes = append(ownTypes, typeDeclarations(loaded, file)...)
		}
	}
	return projectIndex{
		request: request, files: files, declared: declared, project: project, own: own, ownTypes: ownTypes,
	}
}

// typeDeclarations is every type declaration of one file, with its comment.
func typeDeclarations(loaded *packages.Package, file *ast.File) []declaredType {
	found := []declaredType{}
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
				comment := mo.Fold(mo.EmptyableToOption(declarations.TypeComment(typeSpec, group)), func(written *ast.CommentGroup) *ast.CommentGroup { return written },
					func(error) *ast.CommentGroup { return emptyComment() })
				found = append(found, declaredType{spec: typeSpec, loaded: loaded, object: object, comment: comment})
			}
		}
	}
	return found
}

func emptyComment() *ast.CommentGroup { return &ast.CommentGroup{List: []*ast.Comment{}} }

// position is where pos is written.
func (index projectIndex) position(pos token.Pos) token.Position {
	return index.files.Position(pos)
}

// sourceOrder orders two positions as they are written: by file name, then by place in the file. A
// token.Pos alone orders files by when the loader parsed them, which differs from run to run.
func (index projectIndex) sourceOrder(left token.Pos, right token.Pos) int {
	leftAt, rightAt := index.position(left), index.position(right)
	return cmp.Or(cmp.Compare(leftAt.Filename, rightAt.Filename), cmp.Compare(leftAt.Offset, rightAt.Offset))
}

// placed is where pos is written, as every backend's refusals name a place.
func (index projectIndex) placed(pos token.Pos) assembling.Position {
	return placedAt(index.position(pos))
}

// placedAt is a Go position as every backend's refusals name a place.
func placedAt(position token.Position) assembling.Position {
	return assembling.Position{File: position.Filename, Line: position.Line, Column: position.Column}
}

// refused is a step refusing the declaration at pos, for one of the reader's own problems or for a
// problem a parser of the description found in what the declaration writes.
func (index projectIndex) refused(pos token.Pos, problem assembling.Problem) found {
	return refusedAt(index.placed(pos), problem)
}
