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
// each with its syntax, and the API's name and where it is given.
type Request struct {
	Packages []*packages.Package
	API      assembling.NamedAPI
}

// Reading is what reading a project's routes found: the import path of every package read,
// sorted, the API the routes read declare, every Go declaration refused, and the Go server plan of
// the routes read. Package assembling decides whether anything was refused, and the plan is
// written only when nothing was.
type Reading struct {
	Read     []string
	Declared assembling.Declared
	Refused  []assembling.Refusal
	Plan     serverplan.Plan
}

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
			switch serverplan.SourceFileAt(files.Position(file.Pos()).Filename).(type) {
			case serverplan.WrittenFile:
				own = append(own, ownFile{loaded: loaded, file: file})
				ownTypes = append(ownTypes, typeDeclarations(loaded, file)...)
			case serverplan.GeneratedFile, nil:
			}
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

// placed is where pos is written, as every backend's refusals name a place.
func (index projectIndex) placed(pos token.Pos) assembling.Position {
	return placedAt(index.position(pos))
}

// placedAt is a Go position as every backend's refusals name a place.
func placedAt(position token.Position) assembling.Position {
	return assembling.Position{File: position.Filename, Line: position.Line, Column: position.Column}
}

// refused is a step refusing the declaration at pos.
func (index projectIndex) refused(pos token.Pos, problem Problem) found {
	return refusedAt(index.placed(pos), problem)
}
