package odin

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

type declarationName struct {
	pkg  string
	name string
}

func nameOf(declaration compiler.Declaration) declarationName {
	return declarationName{pkg: declaration.Entity.Package, name: declaration.Entity.Name}
}

type documentedProject struct {
	typeDirectives map[declarationName]map[string]writtenDirective
	routes         []routeSource
}

type routeSource struct {
	declaration compiler.Declaration
	method      description.Method
	path        description.RoutePath
	guard       outcome.Option[guardDirective]
	headers     []headerDirective
}

type headerDirective struct {
	field string
	name  string
}

type guardDirective struct {
	target  declarationName
	scope   string
	written string
}

type writtenDirective struct {
	comment   string
	arguments []string
}

type declarationDirectives struct {
	single  map[string]writtenDirective
	headers []writtenDirective
}

// RouteArgumentsWrong preserves the directive and procedure when a route needs a method and path.
type RouteArgumentsWrong struct {
	Position  assembling.Position
	Procedure string
	Written   string
}

// Error describes the route directive's expected arguments.
func (failure RouteArgumentsWrong) Error() string {
	return fmt.Sprintf("%s Odin procedure %s: %q requires METHOD /path", failure.Position, failure.Procedure, failure.Written)
}

func readDocumentation(declarations []compiler.Declaration) outcome.Result[documentedProject] {
	packages := map[string]string{}
	read := outcome.Ok(documentedProject{typeDirectives: map[declarationName]map[string]writtenDirective{}, routes: []routeSource{}})
	for _, declaration := range declarations {
		directory := filepath.Dir(declaration.File)
		if previous, present := packages[declaration.Entity.Package]; present && previous != directory {
			return outcome.Err[documentedProject](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Package, Rule: "package name also belongs to " + previous + "; native package names must be unique across project packages"})
		}
		packages[declaration.Entity.Package] = directory
		read = outcome.Fold(read, func(project documentedProject) outcome.Result[documentedProject] {
			return outcome.Fold(readDirectives(declaration), func(directives declarationDirectives) outcome.Result[documentedProject] {
				if declaration.Entity.Kind == "Type" && len(directives.single) != 0 {
					project.typeDirectives[nameOf(declaration)] = directives.single
				}
				return outcome.Fold(readRouteSource(declaration, directives), func(route outcome.Option[routeSource]) outcome.Result[documentedProject] {
					outcome.ForEachOption(route, func(route routeSource) { project.routes = append(project.routes, route) })
					return outcome.Ok(project)
				}, outcome.Err[documentedProject])
			}, outcome.Err[documentedProject])
		}, outcome.Err[documentedProject])
	}
	return read
}

func readDirectives(declaration compiler.Declaration) outcome.Result[declarationDirectives] {
	directives := declarationDirectives{single: map[string]writtenDirective{}, headers: []writtenDirective{}}
	refuse := func(rule string) outcome.Result[declarationDirectives] {
		return outcome.Err[declarationDirectives](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: rule})
	}
	for _, comment := range declaration.Comments {
		body, matched := strings.CutPrefix(comment, "//nocontract:")
		if !matched {
			continue
		}
		words := strings.Fields(body)
		if len(words) == 0 {
			return refuse("unknown directive " + comment)
		}
		name := words[0]
		switch name {
		case "route", "guard", "header":
			if declaration.Entity.Kind != "Procedure" {
				return refuse(comment + " belongs on a procedure")
			}
		case "status", "instant":
			if declaration.Entity.Kind != "Type" {
				return refuse(comment + " belongs on a type")
			}
		default:
			return refuse("unknown directive " + comment)
		}
		written := writtenDirective{comment: comment, arguments: words[1:]}
		if name == "header" {
			directives.headers = append(directives.headers, written)
			continue
		}
		if first, already := directives.single[name]; already {
			return refuse(comment + " repeats " + first.comment)
		}
		directives.single[name] = written
	}
	return outcome.Ok(directives)
}

func readRouteSource(declaration compiler.Declaration, directives declarationDirectives) outcome.Result[outcome.Option[routeSource]] {
	route, routed := directives.single["route"]
	guard, guarded := directives.single["guard"]
	refuse := func(rule string) outcome.Result[outcome.Option[routeSource]] {
		return outcome.Err[outcome.Option[routeSource]](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: rule})
	}
	if !routed {
		if guarded {
			return refuse(guard.comment + " requires a route on the same procedure")
		}
		if len(directives.headers) > 0 {
			return refuse(directives.headers[0].comment + " requires a route on the same procedure")
		}
		return outcome.Ok(outcome.None[routeSource]())
	}
	if len(route.arguments) != 2 {
		return outcome.Err[outcome.Option[routeSource]](RouteArgumentsWrong{Position: placed(declaration), Procedure: declaration.Entity.Name, Written: route.comment})
	}
	headers := []headerDirective{}
	for _, header := range directives.headers {
		if len(header.arguments) != 2 {
			return refuse(header.comment + " requires field Header-Name")
		}
		headers = append(headers, headerDirective{field: header.arguments[0], name: header.arguments[1]})
	}
	return outcome.FoldOption(description.ParseMethod(route.arguments[0]), func(method description.Method) outcome.Result[outcome.Option[routeSource]] {
		switch path := description.ParseRoutePath(route.arguments[1]).(type) {
		case description.RoutePathRead:
			return outcome.Fold(readGuardDirective(keyed.Entry(directives.single, "guard"), declaration), func(guard outcome.Option[guardDirective]) outcome.Result[outcome.Option[routeSource]] {
				return outcome.Ok(outcome.Some(routeSource{declaration: declaration, method: method, path: path.Path, guard: guard, headers: headers}))
			}, outcome.Err[outcome.Option[routeSource]])
		case description.RoutePathRefused:
			return refuse(path.Problem.Report(indexLanguage()).Message)
		case nil:
		}
		return refuse("route path has no parsed outcome")
	}, func() outcome.Result[outcome.Option[routeSource]] {
		return refuse("unsupported HTTP method " + route.arguments[0])
	})
}

func readGuardDirective(written outcome.Option[writtenDirective], declaration compiler.Declaration) outcome.Result[outcome.Option[guardDirective]] {
	return outcome.FoldOption(written, func(guard writtenDirective) outcome.Result[outcome.Option[guardDirective]] {
		if len(guard.arguments) == 2 {
			pkg, procedure, named := strings.Cut(guard.arguments[0], ".")
			if named && pkg != "" && procedure != "" && !strings.Contains(procedure, ".") {
				return outcome.Ok(outcome.Some(guardDirective{target: declarationName{pkg: pkg, name: procedure}, scope: guard.arguments[1], written: guard.comment}))
			}
		}
		return outcome.Err[outcome.Option[guardDirective]](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: guard.comment + " requires package.procedure scope"})
	}, func() outcome.Result[outcome.Option[guardDirective]] {
		return outcome.Ok(outcome.None[guardDirective]())
	})
}

func reflectedProcedures(declarations []compiler.Declaration, routes []routeSource) []compiler.Declaration {
	selected := map[declarationName]struct{}{}
	for _, route := range routes {
		selected[nameOf(route.declaration)] = struct{}{}
		outcome.ForEachOption(route.guard, func(guard guardDirective) { selected[guard.target] = struct{}{} })
	}
	procedures := []compiler.Declaration{}
	for _, declaration := range declarations {
		if _, needed := selected[nameOf(declaration)]; needed && declaration.Entity.Kind == "Procedure" {
			procedures = append(procedures, declaration)
		}
	}
	return procedures
}

func (project documentedProject) typeDirective(declaration compiler.Declaration, name string) outcome.Option[string] {
	return outcome.FoldOption(keyed.Entry(project.typeDirectives[nameOf(declaration)], name), func(written writtenDirective) outcome.Option[string] {
		return outcome.Some(strings.Join(written.arguments, " "))
	}, outcome.None[string])
}
