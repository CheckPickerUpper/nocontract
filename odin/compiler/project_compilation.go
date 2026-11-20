// Package compiler reads Odin's checked declarations and runtime type information.
package compiler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/scanner"

	"github.com/CheckPickerUpper/outcome"
)

// Attribute is an attribute the compiler resolved on a declaration.
type Attribute struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Entity is an active declaration exported by the compiler.
type Entity struct {
	Name       string      `json:"name"`
	Kind       string      `json:"kind"`
	Package    string      `json:"pkg"`
	File       int         `json:"file"`
	Offset     int         `json:"offset"`
	Attributes []Attribute `json:"attributes"`
}

// Semantics is the source identity of compiler-checked declarations.
type Semantics struct {
	Files    []string `json:"files"`
	Entities []Entity `json:"entities"`
}

// Declaration is one compiler-checked source declaration with its attached metadata.
type Declaration struct {
	Entity   Entity
	File     string
	Line     int
	Column   int
	Comments []string
}

// Field is one field of a compiler-resolved struct.
type Field struct {
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Tag   string `json:"tag"`
	Using bool   `json:"using"`
}

// compilerTypeJSON is interpreted once into the closed Type algebra.
type compilerTypeJSON struct {
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Package    string   `json:"package"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Column     int      `json:"column"`
	Size       int      `json:"size"`
	Base       int      `json:"base"`
	Element    int      `json:"element"`
	Key        int      `json:"key"`
	Length     int      `json:"length"`
	Signed     bool     `json:"signed"`
	NoNil      bool     `json:"no_nil"`
	Fields     []Field  `json:"fields"`
	Variants   []int    `json:"variants"`
	Names      []string `json:"names"`
	Values     []uint64 `json:"values"`
	Parameters []int    `json:"parameters"`
	Results    []int    `json:"results"`
}

// Root names a procedure whose compiler type starts a reflection graph.
type Root struct {
	Package string `json:"package"`
	Name    string `json:"name"`
	Type    int    `json:"type"`
}

// Graph is every native type reachable from the project's route and guard procedures.
type Graph struct {
	Types []Type
	Roots []Root
}

type graphJSON struct {
	Types []compilerTypeJSON `json:"types"`
	Roots []Root             `json:"roots"`
}

// CommandFailed retains the exact command, directory, output, and operating system cause.
type CommandFailed struct {
	Directory      string
	Arguments      []string
	Output         string
	StandardOutput string
	Cause          error
}

// Error describes the compiler command and its diagnostics.
func (failure CommandFailed) Error() string {
	return fmt.Sprintf("Odin command %q in %s: %s: %v", failure.Arguments, failure.Directory, failure.Output, failure.Cause)
}

// Unwrap preserves the process failure.
func (failure CommandFailed) Unwrap() error { return failure.Cause }

// SourceUnreadable retains the source the operating system could not read.
type SourceUnreadable struct {
	Path  string
	Cause error
}

// SourceOffsetInvalid is a compiler source offset outside the file it identifies.
type SourceOffsetInvalid struct {
	Path   string
	Offset int
	Length int
}

// Error reports the compiler offset and the source length it exceeds.
func (failure SourceOffsetInvalid) Error() string {
	return fmt.Sprintf("compiler offset %d exceeds %s source length %d", failure.Offset, failure.Path, failure.Length)
}

// Error describes the source operation that failed.
func (failure SourceUnreadable) Error() string {
	return fmt.Sprintf("reading Odin source %s: %v", failure.Path, failure.Cause)
}

// Unwrap preserves the source failure.
func (failure SourceUnreadable) Unwrap() error { return failure.Cause }

// SourceUnwritable retains a generated source file the operating system refused.
type SourceUnwritable struct {
	Path     string
	Contents []byte
	Cause    error
}

// Error reports the source write that failed.
func (failure SourceUnwritable) Error() string {
	return fmt.Sprintf("writing Odin source %s: %v", failure.Path, failure.Cause)
}

// Unwrap preserves the operating system's write failure.
func (failure SourceUnwritable) Unwrap() error { return failure.Cause }

// ExportUndecodable retains the complete compiler export the JSON codec refused.
type ExportUndecodable struct {
	Path     string
	Contents []byte
	Cause    error
}

// Error reports the export that failed to decode.
func (failure ExportUndecodable) Error() string {
	return fmt.Sprintf("decoding Odin compiler export %s: %v", failure.Path, failure.Cause)
}

// Unwrap preserves the JSON codec's failure.
func (failure ExportUndecodable) Unwrap() error { return failure.Cause }

// ReflectionFailed retains the generated reflection source and the compiler failure.
type ReflectionFailed struct {
	Path   string
	Source string
	Cause  error
}

// Error reports the reflection compilation that failed.
func (failure ReflectionFailed) Error() string {
	return fmt.Sprintf("compiling Odin reflection %s: %v", failure.Path, failure.Cause)
}

// Unwrap preserves the compiler's full command and diagnostics.
func (failure ReflectionFailed) Unwrap() error { return failure.Cause }

// Flags is the compiler's strictness shared by checking, reflection, and generated bindings.
func Flags(packages []string) []string {
	return []string{"-vet", "-vet-packages:" + strings.Join(packages, ","), "-vet-cast", "-vet-using-param", "-vet-unused-procedures", "-strict-style", "-disallow-do", "-warnings-as-errors", "-custom-attribute:error_code,not_an_error"}
}

// Run captures stdout separately so compiler diagnostics cannot become protocol data.
func Run(context context.Context, directory string, arguments []string) outcome.Result[[]byte] {
	command := exec.CommandContext(context, arguments[0], arguments[1:]...)
	command.Dir = directory
	diagnostics := &strings.Builder{}
	command.Stderr = diagnostics
	output, runError := command.Output()
	return outcome.Fold(outcome.TupleToResult(output, runError), outcome.Ok[[]byte], func(cause error) outcome.Result[[]byte] {
		return outcome.Err[[]byte](CommandFailed{Directory: directory, Arguments: slices.Clone(arguments), Output: diagnostics.String(), StandardOutput: string(output), Cause: cause})
	})
}

// ReadDeclarations checks the packages and retrieves source identities and attached comments.
func ReadDeclarations(context context.Context, directory, scratch string, packages []string) outcome.Result[[]Declaration] {
	declarations := outcome.Ok([]Declaration{})
	for index, path := range packages {
		declarations = outcome.Fold(declarations, func(previous []Declaration) outcome.Result[[]Declaration] {
			export := filepath.Join(scratch, fmt.Sprintf("semantics-%d.json", index))
			arguments := append([]string{"odin", "check", filepath.Join(directory, path), "-no-entry-point"}, Flags([]string{"nocontract_probe"})...)
			arguments = append(arguments, "-export-semantics:json", "-export-semantics-file:"+export)
			return outcome.Fold(Run(context, directory, arguments), func([]byte) outcome.Result[[]Declaration] {
				return outcome.Fold(decodeFile(export, Semantics{Files: []string{}, Entities: []Entity{}}), func(semantics Semantics) outcome.Result[[]Declaration] {
					read := outcome.Ok(previous)
					for _, entity := range semantics.Entities {
						if entity.File < 0 || entity.File >= len(semantics.Files) || entity.Offset < 0 {
							continue
						}
						if entity.Kind != "Type" && entity.Kind != "Procedure" {
							continue
						}
						file := semantics.Files[entity.File]
						if filepath.Clean(filepath.Dir(file)) != filepath.Clean(filepath.Join(directory, path)) {
							continue
						}
						read = outcome.Fold(read, func(loaded []Declaration) outcome.Result[[]Declaration] {
							return outcome.Fold(sourceDeclaration(file, entity), func(declaration Declaration) outcome.Result[[]Declaration] {
								return outcome.Ok(append(loaded, declaration))
							}, outcome.Err[[]Declaration])
						}, outcome.Err[[]Declaration])
					}
					return read
				}, outcome.Err[[]Declaration])
			}, outcome.Err[[]Declaration])
		}, outcome.Err[[]Declaration])
	}
	return declarations
}

func decodeFile[T any](path string, value T) outcome.Result[T] {
	return outcome.Fold(outcome.TupleToResult(os.ReadFile(path)), func(content []byte) outcome.Result[T] {
		return outcome.FoldOption(outcome.EmptyableToOption(json.Unmarshal(content, &value)), func(cause error) outcome.Result[T] {
			return outcome.Err[T](ExportUndecodable{Path: path, Contents: content, Cause: cause})
		}, func() outcome.Result[T] { return outcome.Ok(value) })
	}, func(cause error) outcome.Result[T] { return outcome.Err[T](SourceUnreadable{Path: path, Cause: cause}) })
}

func sourceDeclaration(file string, entity Entity) outcome.Result[Declaration] {
	return outcome.Fold(outcome.TupleToResult(os.ReadFile(file)), func(content []byte) outcome.Result[Declaration] {
		if entity.Offset > len(content) {
			return outcome.Err[Declaration](SourceOffsetInvalid{Path: file, Offset: entity.Offset, Length: len(content)})
		}
		prefix := string(content[:entity.Offset])
		line := strings.Count(prefix, "\n") + 1
		column := len(prefix) - strings.LastIndex(prefix, "\n")
		return outcome.Ok(Declaration{Entity: entity, File: file, Line: line, Column: column, Comments: sourceComments(prefix)})
	}, func(cause error) outcome.Result[Declaration] {
		return outcome.Err[Declaration](SourceUnreadable{Path: file, Cause: cause})
	})
}

type sourceToken struct {
	kind rune
	text string
}

func sourceComments(prefix string) []string {
	// Odin already checked syntax. Go-specific literal diagnostics do not change
	// the comment, quote, and punctuation boundaries used for attached metadata.
	lexer := &scanner.Scanner{
		Error:       func(*scanner.Scanner, string) {},
		ErrorCount:  0,
		Mode:        scanner.ScanStrings | scanner.ScanRawStrings | scanner.ScanChars | scanner.ScanComments,
		Whitespace:  scanner.GoWhitespace,
		IsIdentRune: func(rune, int) bool { return false },
		Position:    scanner.Position{Filename: "", Offset: 0, Line: 1, Column: 1},
	}
	lexer.Init(strings.NewReader(prefix))
	lexer.Mode = scanner.ScanStrings | scanner.ScanRawStrings | scanner.ScanChars | scanner.ScanComments
	tokens := []sourceToken{}
	for kind := lexer.Scan(); kind != scanner.EOF; kind = lexer.Scan() {
		tokens = append(tokens, sourceToken{kind: kind, text: lexer.TokenText()})
	}
	comments := []string{}
	for position := len(tokens) - 1; position >= 0; {
		token := tokens[position]
		switch token.kind {
		case scanner.Comment:
			if strings.HasPrefix(token.text, "//") {
				comments = append(comments, token.text)
			}
			position--
		case ')':
			opened := attributeStart(tokens, position)
			if opened < 0 {
				slices.Reverse(comments)
				return comments
			}
			position = opened - 1
		default:
			slices.Reverse(comments)
			return comments
		}
	}
	slices.Reverse(comments)
	return comments
}

func attributeStart(tokens []sourceToken, end int) int {
	depth := 1
	for position := end - 1; position >= 0; position-- {
		switch tokens[position].kind {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				if position > 0 && tokens[position-1].kind == '@' {
					return position - 1
				}
				return -1
			}
		}
	}
	return -1
}

// ReflectProcedures compiles a reflection program against the selected procedures.
func ReflectProcedures(context context.Context, directory, scratch string, packages []string, roots []Declaration) outcome.Result[Graph] {
	if len(roots) == 0 {
		return outcome.Ok(Graph{Types: []Type{}, Roots: []Root{}})
	}
	source := &strings.Builder{}
	source.WriteString(reflectionSource)
	importFailures := []error{}
	for index, path := range packages {
		relative := outcome.Fold(outcome.TupleToResult(filepath.Rel(scratch, filepath.Join(directory, path))), func(path string) string { return path }, func(cause error) string {
			importFailures = append(importFailures, ImportUnresolved{From: scratch, Package: filepath.Join(directory, path), Cause: cause})
			return ""
		})
		alias := "_"
		for _, declaration := range roots {
			if filepath.Dir(declaration.File) == filepath.Clean(filepath.Join(directory, path)) {
				alias = fmt.Sprintf("nc_package_%d", index)
			}
		}
		fmt.Fprintf(source, "import %s %s\n", alias, strconv.Quote(filepath.ToSlash(relative)))
	}
	if len(importFailures) > 0 {
		return outcome.Err[Graph](errors.Join(importFailures...))
	}
	source.WriteString("\nmain :: proc() {\n graph := Graph{types = {}, roots = {}}\n queue := make([dynamic]^runtime.Type_Info)\n defer delete(queue)\n known := make(map[typeid]int)\n defer delete(known)\n")
	for _, declaration := range roots {
		index := slices.IndexFunc(packages, func(path string) bool {
			return filepath.Clean(filepath.Join(directory, path)) == filepath.Dir(declaration.File)
		})
		fmt.Fprintf(source, " append(&graph.roots, Root{package_name = %s, name = %s, type = remember_type(type_info_of(type_of(nc_package_%d.%s)), &known, &queue)})\n", strconv.Quote(declaration.Entity.Package), strconv.Quote(declaration.Entity.Name), index, declaration.Entity.Name)
	}
	source.WriteString(" for index := 0; index < len(queue); index += 1 { append(&graph.types, describe_type(queue[index], &known, &queue)) }\n output, failure := json.marshal(graph, {spec = .JSON, use_enum_names = true})\n switch cause in failure {\n case nil: fmt.println(string(output))\n case json.Marshal_Data_Error, io.Error: fmt.eprintln(cause); os.exit(1)\n }\n}\n")
	path := filepath.Join(scratch, "reflection.odin")
	return outcome.FoldOption(outcome.EmptyableToOption(os.WriteFile(path, []byte(source.String()), 0o644)), func(cause error) outcome.Result[Graph] {
		return outcome.Err[Graph](SourceUnwritable{Path: path, Contents: []byte(source.String()), Cause: cause})
	}, func() outcome.Result[Graph] {
		arguments := append([]string{"odin", "run", scratch}, Flags([]string{"nocontract_probe"})...)
		arguments = append(arguments, "-out:"+filepath.Join(scratch, "reflection"))
		return outcome.Fold(Run(context, directory, arguments), func(content []byte) outcome.Result[Graph] {
			graph := graphJSON{Types: []compilerTypeJSON{}, Roots: []Root{}}
			return outcome.FoldOption(outcome.EmptyableToOption(json.Unmarshal(content, &graph)), func(cause error) outcome.Result[Graph] {
				return outcome.Err[Graph](ExportUndecodable{Path: path, Contents: content, Cause: cause})
			}, func() outcome.Result[Graph] {
				types := []Type{}
				for _, checked := range graph.Types {
					types = append(types, resolvedType(checked))
				}
				return outcome.Ok(Graph{Types: types, Roots: graph.Roots})
			})
		}, func(cause error) outcome.Result[Graph] {
			return outcome.Err[Graph](ReflectionFailed{Path: path, Source: source.String(), Cause: cause})
		})
	})
}

// ImportUnresolved is a native import that cannot be expressed from its generated package.
type ImportUnresolved struct {
	From    string
	Package string
	Cause   error
}

// Error describes the two paths the relative import could not connect.
func (failure ImportUnresolved) Error() string {
	return fmt.Sprintf("Odin import from %s to %s: %v", failure.From, failure.Package, failure.Cause)
}

// Unwrap preserves the operating system's path failure.
func (failure ImportUnresolved) Unwrap() error { return failure.Cause }
