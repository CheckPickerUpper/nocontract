// Package reading reads a Go project's //nocontract:route handlers, their inputs and the members
// of their outcomes out of Go's type checker, as the language-neutral description and the Go
// server plan, and refuses every declaration the generated server and its client cannot hold.
//
// Every reading step is a function returning its value and everything it found: refusals, the
// members and named structs it read, and the packages whose declarations it read. The checks
// that span routes, such as two routes serving one pattern or two declarations taking one
// TypeScript name, run once over everything found, in assembling.go.
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

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// Request is what the generator reads: the loaded packages, the project's own and nocontract's,
// each with its syntax, and the name of the Effect API.
type Request struct {
	Packages []*packages.Package
	APIName  string
}

// Reading is the import path of every package read, sorted, and what reading the routes ended as.
type Reading struct {
	Read   []string
	Routes RoutesOutcome
}

// RoutesOutcome is what reading a project's routes ended as.
//
//sumtype:decl
type RoutesOutcome interface{ isRoutesOutcome() }

// RoutesRead is every route, read without a refusal, as the description and the Go server plan.
//
//ergo:not-an-error
type RoutesRead struct {
	API  description.API
	Plan serverplan.Plan
}

// RoutesRefused is every declaration that could not be read, in source order.
//
//ergo:code NC-1031
type RoutesRefused struct {
	Refusals []Refusal
}

func (RoutesRead) isRoutesOutcome()    {}
func (RoutesRefused) isRoutesOutcome() {}

// reservedNames are the names the TypeScript file imports, which no declaration may take.
func reservedNames() []string {
	return []string{"Schema", "HttpApi", "HttpApiEndpoint", "HttpApiGroup", "HttpApiSchema"}
}

// ReadRoutes reads every route the project's packages declare.
func ReadRoutes(request Request) Reading {
	index := indexOf(request)
	shared := readShared(index)
	guards := readGuards(index)
	return assemble(index, shared.found.with(guards.found), guards, readRoutes(index, guards))
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

// sharedFailures names nocontract's own failures: RequestRefused, which a route that reads its
// request can answer before its handler runs, and AnswerNotWritten, which every route can answer.
type sharedFailures struct {
	requestRefused   string
	answerNotWritten string
}

// ownFile is a Go file the project wrote, and its package.
type ownFile struct {
	loaded *packages.Package
	file   *ast.File
}

// projectIndex is what every reading step reads from: the request, the file set positions are
// read through, every type declaration of the loaded syntax, the project's own packages in import
// path order, the files of them the project wrote and the types those declare, and nocontract's
// own failures.
type projectIndex struct {
	request  Request
	files    *token.FileSet
	declared map[typeKey]declaredType
	project  []*packages.Package
	own      []ownFile
	ownTypes []declaredType
	shared   sharedFailures
}

// indexOf indexes the loaded packages once.
func indexOf(request Request) projectIndex {
	files := token.NewFileSet()
	declared := map[typeKey]declaredType{}
	project := []*packages.Package{}
	for _, loaded := range request.Packages {
		files = loaded.Fset
		for _, file := range loaded.Syntax {
			for _, entry := range typeDeclarations(loaded, file) {
				declared[keyOf(entry.object)] = entry
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
		shared: sharedFailures{requestRefused: "RequestRefused", answerNotWritten: "AnswerNotWritten"},
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

// refused is a step refusing the declaration at pos.
func (index projectIndex) refused(pos token.Pos, problem Problem) found {
	return refusedAt(index.position(pos), problem)
}

// readShared reads the runtime's sealed SharedFailure: every failure nocontract answers on a
// route's behalf, each a member routes can answer.
func readShared(index projectIndex) reading[sealedRead] {
	return alongside(readSealed(index, typeKey{path: serverplan.NoContractPath, name: serverplan.SharedFailureType}, token.NoPos),
		packageRead(serverplan.NoContractPath))
}
