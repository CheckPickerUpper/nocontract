// Package compiler reads the resolved public surface exported by the installed Gleam compiler.
package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"time"

	"github.com/CheckPickerUpper/nocontract/internal/sequence"
	"github.com/CheckPickerUpper/outcome"
)

// Identity is the compiler's exact identity of a named type.
type Identity struct {
	Package string
	Module  string
	Name    string
}

// Type is one resolved compiler type.
//
//sumtype:decl
type Type interface{ isType() }

// Named is a named type and its resolved type arguments.
//
//ergo:not-an-error
type Named struct {
	Identity  Identity
	Arguments []Type
}

// Tuple is an ordered tuple.
//
//ergo:not-an-error
type Tuple struct{ Elements []Type }

// FunctionType is a resolved function type.
//
//ergo:not-an-error
type FunctionType struct {
	Parameters []Type
	Returning  Type
}

// Variable is a type variable that has not been instantiated.
//
//ergo:not-an-error
type Variable struct{ ID int }

func (Named) isType()        {}
func (Tuple) isType()        {}
func (FunctionType) isType() {}
func (Variable) isType()     {}

// Parameter is a compiler-resolved parameter and its optional label.
type Parameter struct {
	Label outcome.Option[string]
	Type  Type
}

// Constructor is a public constructor with the fields resolved by the compiler.
type Constructor struct {
	Name          string
	Documentation string
	Parameters    []Parameter
}

// Definition is a public custom type.
type Definition struct {
	Identity     Identity
	Parameters   int
	Constructors []Constructor
}

// Function is a public function's resolved signature.
type Function struct {
	Documentation string
	Parameters    []Parameter
	Returning     Type
}

// Module is the public types and functions of one module.
type Module struct {
	Types     map[string]Definition
	Functions map[string]Function
}

// Package is the compiler's checked package interface.
type Package struct {
	Name    string
	Modules map[string]Module
}

// ExportRefused preserves the command, output, cause and measured compiler time.
type ExportRefused struct {
	Directory   string
	Arguments   []string
	Output      string
	Diagnostics string
	Duration    time.Duration
	Cause       error
}

func (failure ExportRefused) Error() string {
	return fmt.Sprintf("Gleam package interface in %s: %v\n%s\n%s", failure.Directory, failure.Cause, failure.Diagnostics, failure.Output)
}
func (failure ExportRefused) Unwrap() error { return failure.Cause }

// InterfaceMalformed preserves the JSON the compiler emitted and its decode error.
type InterfaceMalformed struct {
	Content []byte
	Cause   error
}

func (failure InterfaceMalformed) Error() string {
	return fmt.Sprintf("Gleam package interface is malformed: %v", failure.Cause)
}
func (failure InterfaceMalformed) Unwrap() error { return failure.Cause }

// TypeMalformed identifies a resolved type whose compiler representation is unsupported.
type TypeMalformed struct {
	Kind    string
	Content rawType
}

func (failure TypeMalformed) Error() string {
	return fmt.Sprintf("Gleam compiler type kind %q is unsupported: %+v", failure.Kind, failure.Content)
}

type rawType struct {
	Kind       string    `json:"kind"`
	Package    string    `json:"package"`
	Module     string    `json:"module"`
	Name       string    `json:"name"`
	Parameters []rawType `json:"parameters"`
	Elements   []rawType `json:"elements"`
	Returning  *rawType  `json:"return"`
	ID         int       `json:"id"`
}
type rawParameter struct {
	Label *string `json:"label"`
	Type  rawType `json:"type"`
}
type rawConstructor struct {
	Name          string         `json:"name"`
	Documentation *string        `json:"documentation"`
	Parameters    []rawParameter `json:"parameters"`
}
type rawDefinition struct {
	Parameters   int              `json:"parameters"`
	Constructors []rawConstructor `json:"constructors"`
}
type rawFunction struct {
	Documentation *string        `json:"documentation"`
	Parameters    []rawParameter `json:"parameters"`
	Returning     rawType        `json:"return"`
}
type rawModule struct {
	Types     map[string]rawDefinition `json:"types"`
	Functions map[string]rawFunction   `json:"functions"`
}
type rawPackage struct {
	Name    string               `json:"name"`
	Modules map[string]rawModule `json:"modules"`
}

// Read asks the installed compiler to resolve the package before reading any signatures.
func Read(context context.Context, directory string) outcome.Result[Package] {
	arguments := []string{"export", "package-interface"}
	command := exec.CommandContext(context, "gleam", arguments...)
	command.Dir = directory
	diagnostics := &bytes.Buffer{}
	output := &bytes.Buffer{}
	command.Stderr = diagnostics
	command.Stdout = output
	started := time.Now()
	return outcome.FoldOption(outcome.EmptyableToOption(command.Run()), func(cause error) outcome.Result[Package] {
		return outcome.Err[Package](ExportRefused{Directory: directory, Arguments: append([]string{"gleam"}, arguments...), Output: output.String(), Diagnostics: diagnostics.String(), Duration: time.Since(started), Cause: cause})
	}, func() outcome.Result[Package] { return Decode(output.Bytes()) })
}

// Decode parses the compiler's JSON once into resolved type variants.
func Decode(content []byte) outcome.Result[Package] {
	written := rawPackage{Name: "", Modules: map[string]rawModule{}}
	return outcome.FoldOption(outcome.EmptyableToOption(json.Unmarshal(content, &written)), func(cause error) outcome.Result[Package] {
		return outcome.Err[Package](InterfaceMalformed{Content: content, Cause: cause})
	}, func() outcome.Result[Package] {
		return outcome.Fold(sequence.Traverse(slices.Sorted(maps.Keys(written.Modules)), func(name string) outcome.Result[Module] {
			return readModule(written.Name, name, written.Modules[name])
		}), func(read []Module) outcome.Result[Package] {
			modules := map[string]Module{}
			for i, name := range slices.Sorted(maps.Keys(written.Modules)) {
				modules[name] = read[i]
			}
			return outcome.Ok(Package{Name: written.Name, Modules: modules})
		}, outcome.Err[Package])
	})
}

func readModule(packageName, moduleName string, written rawModule) outcome.Result[Module] {
	names := slices.Sorted(maps.Keys(written.Types))
	return outcome.Fold(sequence.Traverse(names, func(name string) outcome.Result[Definition] {
		declared := written.Types[name]
		return outcome.Fold(sequence.Traverse(declared.Constructors, func(constructor rawConstructor) outcome.Result[Constructor] {
			return outcome.Fold(readParameters(constructor.Parameters), func(parameters []Parameter) outcome.Result[Constructor] {
				documentation := outcome.FoldOption(outcome.PointerToOption(constructor.Documentation), func(text string) string { return text }, func() string { return "" })
				return outcome.Ok(Constructor{Name: constructor.Name, Documentation: documentation, Parameters: parameters})
			}, outcome.Err[Constructor])
		}), func(constructors []Constructor) outcome.Result[Definition] {
			return outcome.Ok(Definition{Identity: Identity{Package: packageName, Module: moduleName, Name: name}, Parameters: declared.Parameters, Constructors: constructors})
		}, outcome.Err[Definition])
	}), func(definitions []Definition) outcome.Result[Module] {
		types := map[string]Definition{}
		for _, definition := range definitions {
			types[definition.Identity.Name] = definition
		}
		functionNames := slices.Sorted(maps.Keys(written.Functions))
		return outcome.Fold(sequence.Traverse(functionNames, func(name string) outcome.Result[Function] {
			declared := written.Functions[name]
			return outcome.Fold(readParameters(declared.Parameters), func(parameters []Parameter) outcome.Result[Function] {
				return outcome.Fold(readType(declared.Returning), func(returning Type) outcome.Result[Function] {
					documentation := outcome.FoldOption(outcome.PointerToOption(declared.Documentation), func(text string) string { return text }, func() string { return "" })
					return outcome.Ok(Function{Documentation: documentation, Parameters: parameters, Returning: returning})
				}, outcome.Err[Function])
			}, outcome.Err[Function])
		}), func(read []Function) outcome.Result[Module] {
			functions := map[string]Function{}
			for i, name := range functionNames {
				functions[name] = read[i]
			}
			return outcome.Ok(Module{Types: types, Functions: functions})
		}, outcome.Err[Module])
	}, outcome.Err[Module])
}

func readParameters(written []rawParameter) outcome.Result[[]Parameter] {
	return sequence.Traverse(written, func(parameter rawParameter) outcome.Result[Parameter] {
		return outcome.Fold(readType(parameter.Type), func(resolved Type) outcome.Result[Parameter] {
			return outcome.Ok(Parameter{Label: outcome.PointerToOption(parameter.Label), Type: resolved})
		}, outcome.Err[Parameter])
	})
}

func readTypes(written []rawType) outcome.Result[[]Type] { return sequence.Traverse(written, readType) }

func readType(written rawType) outcome.Result[Type] {
	switch written.Kind {
	case "named":
		return outcome.Fold(readTypes(written.Parameters), func(parameters []Type) outcome.Result[Type] {
			return outcome.Ok[Type](Named{Identity: Identity{Package: written.Package, Module: written.Module, Name: written.Name}, Arguments: parameters})
		}, outcome.Err[Type])
	case "tuple":
		return outcome.Fold(readTypes(written.Elements), func(elements []Type) outcome.Result[Type] { return outcome.Ok[Type](Tuple{Elements: elements}) }, outcome.Err[Type])
	case "variable":
		return outcome.Ok[Type](Variable{ID: written.ID})
	case "fn":
		return outcome.FoldOption(outcome.PointerToOption(written.Returning), func(returning rawType) outcome.Result[Type] {
			return outcome.Fold(readTypes(written.Parameters), func(parameters []Type) outcome.Result[Type] {
				return outcome.Fold(readType(returning), func(returning Type) outcome.Result[Type] {
					return outcome.Ok[Type](FunctionType{Parameters: parameters, Returning: returning})
				}, outcome.Err[Type])
			}, outcome.Err[Type])
		}, func() outcome.Result[Type] {
			return outcome.Err[Type](TypeMalformed{Kind: written.Kind, Content: written})
		})
	}
	return outcome.Err[Type](TypeMalformed{Kind: written.Kind, Content: written})
}
