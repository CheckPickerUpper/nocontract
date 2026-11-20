// Package authored reads compiler-attached metadata and refines source locations for diagnostics.
package authored

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/gleam/sequence"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/outcome"
)

// Directive is a NoContract directive and the exact line that wrote it.
type Directive struct {
	Name     string
	Words    []string
	Position assembling.Position
}

// Declaration is the metadata attached to a public declaration.
type Declaration struct {
	Position   assembling.Position
	Directives []Directive
}

// Module is a module's public function and constructor metadata.
type Module struct {
	Name         string
	Functions    map[string]Declaration
	Constructors map[string]Declaration
}

// Unreadable preserves the source file and read failure.
type Unreadable struct {
	Path  string
	Cause error
}

func (failure Unreadable) Error() string {
	return fmt.Sprintf("read Gleam metadata %s: %v", failure.Path, failure.Cause)
}
func (failure Unreadable) Unwrap() error { return failure.Cause }

// Read reads directives only from the documentation attached by the compiler.
func Read(directory string, checked compiler.Package) outcome.Result[map[string]Module] {
	return outcome.Fold(sequence.Traverse(slices.Sorted(maps.Keys(checked.Modules)), func(name string) outcome.Result[Module] {
		path := filepath.Join(directory, "src", filepath.FromSlash(name)+".gleam")
		return outcome.Fold(outcome.TupleToResult(os.ReadFile(path)), func(content []byte) outcome.Result[Module] {
			return outcome.Ok(readModule(name, path, string(content), checked.Modules[name]))
		}, func(cause error) outcome.Result[Module] {
			return outcome.Err[Module](Unreadable{Path: path, Cause: cause})
		})
	}), func(read []Module) outcome.Result[map[string]Module] {
		indexed := map[string]Module{}
		for _, module := range read {
			indexed[module.Name] = module
		}
		return outcome.Ok(indexed)
	}, outcome.Err[map[string]Module])
}

func readModule(name, path, content string, checked compiler.Module) Module {
	module := Module{Name: name, Functions: map[string]Declaration{}, Constructors: map[string]Declaration{}}
	for function, signature := range checked.Functions {
		position := sourcePosition(path, content, "pub fn "+function+"(")
		module.Functions[function] = readDocumentation(signature.Documentation, position)
	}
	for _, definition := range checked.Types {
		for _, constructor := range definition.Constructors {
			position := sourcePosition(path, content, constructor.Name)
			module.Constructors[constructor.Name] = readDocumentation(constructor.Documentation, position)
		}
	}
	return module
}

func readDocumentation(documentation string, position assembling.Position) Declaration {
	directives := []Directive{}
	for _, line := range strings.Split(documentation, "\n") {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "nocontract:") {
			words := strings.Fields(strings.TrimPrefix(text, "nocontract:"))
			if len(words) > 0 {
				directives = append(directives, Directive{Name: words[0], Words: words[1:], Position: position})
			}
		}
	}
	return Declaration{Position: position, Directives: directives}
}

func sourcePosition(path, content, prefix string) assembling.Position {
	for offset, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return assembling.Position{File: path, Line: offset + 1, Column: len(line) - len(strings.TrimLeft(line, " \t")) + 1}
		}
	}
	return assembling.Position{File: path, Line: 1, Column: 1}
}

// Constructor locates a constructor by its compiler identity and exact constructor name.
func Constructor(modules map[string]Module, identity compiler.Identity, name string) outcome.Option[Declaration] {
	return outcome.FoldOption(keyed.Entry(modules, identity.Module), func(module Module) outcome.Option[Declaration] {
		return keyed.Entry(module.Constructors, name)
	}, outcome.None[Declaration])
}
