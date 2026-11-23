// Package directives parses NoContract directives attached by the Gleam compiler.
package directives

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/nocontract/internal/sequence"
	"github.com/CheckPickerUpper/outcome"
)

// Declaration identifies the documented source declaration and its attached directives.
type Declaration struct {
	Position   assembling.Position
	Directives []string
}

// Route is a declared route function with its HTTP method, path, headers and optional guard.
type Route struct {
	Declaration Declaration
	Method      description.Method
	Path        description.RoutePath
	Headers     []Header
	Guard       outcome.Option[Guard]
}

// Constructor is a constructor's optional response status and wire spelling.
type Constructor struct {
	Declaration Declaration
	Status      outcome.Option[description.Status]
	Literal     outcome.Option[string]
}

// Module holds a module's documented routes and constructor metadata.
type Module struct {
	Name         string
	Routes       map[string]Route
	Constructors map[string]Constructor
}

// SourceUnreadable preserves a source file path and read failure.
type SourceUnreadable struct {
	Path  string
	Cause error
}

// Error identifies the Gleam source file whose declaration could not be located.
func (failure SourceUnreadable) Error() string {
	return fmt.Sprintf("read Gleam source %s: %v", failure.Path, failure.Cause)
}

// Unwrap returns the source file read error.
func (failure SourceUnreadable) Unwrap() error { return failure.Cause }

// Read parses compiler-attached documentation before any route is selected.
func Read(directory string, checked compiler.Package) outcome.Result[map[string]Module] {
	return outcome.Fold(sequence.Traverse(slices.Sorted(maps.Keys(checked.Modules)), func(name string) outcome.Result[Module] {
		path := filepath.Join(directory, "src", filepath.FromSlash(name)+".gleam")
		return outcome.Fold(outcome.TupleToResult(os.ReadFile(path)), func(content []byte) outcome.Result[Module] {
			return readModule(name, path, string(content), checked.Modules[name])
		}, func(cause error) outcome.Result[Module] {
			return outcome.Err[Module](SourceUnreadable{Path: path, Cause: cause})
		})
	}), func(read []Module) outcome.Result[map[string]Module] {
		indexed := make(map[string]Module, len(read))
		for _, module := range read {
			indexed[module.Name] = module
		}
		return outcome.Ok(indexed)
	}, outcome.Err[map[string]Module])
}

func readModule(name, path, content string, checked compiler.Module) outcome.Result[Module] {
	module := Module{Name: name, Routes: map[string]Route{}, Constructors: map[string]Constructor{}}
	locations := locateDeclarations(path, content)
	return outcome.Fold(sequence.Traverse(slices.Sorted(maps.Keys(checked.Functions)), func(function string) outcome.Result[struct{}] {
		position := locations.position(locations.functions, function)
		return outcome.Fold(functionRoute(checked.Functions[function].Documentation, position), func(route outcome.Option[Route]) outcome.Result[struct{}] {
			outcome.ForEachOption(route, func(found Route) { module.Routes[function] = found })
			return outcome.Ok(struct{}{})
		}, outcome.Err[struct{}])
	}), func([]struct{}) outcome.Result[Module] {
		constructors := []compiler.Constructor{}
		for _, name := range slices.Sorted(maps.Keys(checked.Types)) {
			constructors = append(constructors, checked.Types[name].Constructors...)
		}
		return outcome.Fold(sequence.Traverse(constructors, func(constructor compiler.Constructor) outcome.Result[struct{}] {
			position := locations.position(locations.constructors, constructor.Name)
			return outcome.Fold(constructorMetadata(constructor.Documentation, position), func(parsed Constructor) outcome.Result[struct{}] {
				module.Constructors[constructor.Name] = parsed
				return outcome.Ok(struct{}{})
			}, outcome.Err[struct{}])
		}), func([]struct{}) outcome.Result[Module] { return outcome.Ok(module) }, outcome.Err[Module])
	}, outcome.Err[Module])
}

func functionRoute(documentation string, position assembling.Position) outcome.Result[outcome.Option[Route]] {
	return outcome.Fold(parseDocumentation(documentation, position), func(directives []attachedDirective) outcome.Result[outcome.Option[Route]] {
		route := outcome.None[routeDirective]()
		guard := outcome.None[Guard]()
		headers := []Header{}
		for _, attached := range directives {
			switch parsed := attached.value.(type) {
			case routeDirective:
				route = outcome.Some(parsed)
			case Guard:
				guard = outcome.Some(parsed)
			case Header:
				headers = append(headers, parsed)
			case statusDirective, literalDirective, nil:
				return outcome.Err[outcome.Option[Route]](directiveMisplaced{Statement: attached.statement, Owner: "a function"})
			}
		}
		return outcome.FoldOption(route, func(parsed routeDirective) outcome.Result[outcome.Option[Route]] {
			return outcome.Ok(outcome.Some(Route{Declaration: declaration(position, directives), Method: parsed.Method, Path: parsed.Path, Headers: headers, Guard: guard}))
		}, func() outcome.Result[outcome.Option[Route]] {
			if len(headers) > 0 {
				return outcome.Err[outcome.Option[Route]](routeRequired{Declaration: declaration(position, directives)})
			}
			return outcome.FoldOption(guard, func(Guard) outcome.Result[outcome.Option[Route]] {
				return outcome.Err[outcome.Option[Route]](routeRequired{Declaration: declaration(position, directives)})
			}, func() outcome.Result[outcome.Option[Route]] { return outcome.Ok(outcome.None[Route]()) })
		})
	}, outcome.Err[outcome.Option[Route]])
}

type routeRequired struct{ Declaration Declaration }

func (failure routeRequired) Error() string {
	return fmt.Sprintf("%s: %q requires a route on the same function", failure.Declaration.Position, failure.Declaration.Directives)
}

func constructorMetadata(documentation string, position assembling.Position) outcome.Result[Constructor] {
	return outcome.Fold(parseDocumentation(documentation, position), func(directives []attachedDirective) outcome.Result[Constructor] {
		status := outcome.None[description.Status]()
		literal := outcome.None[string]()
		for _, attached := range directives {
			switch parsed := attached.value.(type) {
			case statusDirective:
				status = outcome.Some(parsed.Status)
			case literalDirective:
				literal = outcome.Some(parsed.Spelling)
			case routeDirective, Guard, Header, nil:
				return outcome.Err[Constructor](directiveMisplaced{Statement: attached.statement, Owner: "a constructor"})
			}
		}
		return outcome.Ok(Constructor{Declaration: declaration(position, directives), Status: status, Literal: literal})
	}, outcome.Err[Constructor])
}

func declaration(position assembling.Position, directives []attachedDirective) Declaration {
	written := make([]string, 0, len(directives))
	for _, directive := range directives {
		written = append(written, directive.statement.Text)
	}
	return Declaration{Position: position, Directives: written}
}

// FindConstructor reads the directives attached to the compiler's named constructor.
func FindConstructor(modules map[string]Module, identity compiler.Identity, name string) outcome.Option[Constructor] {
	return outcome.FoldOption(keyed.Entry(modules, identity.Module), func(module Module) outcome.Option[Constructor] {
		return keyed.Entry(module.Constructors, name)
	}, outcome.None[Constructor])
}
