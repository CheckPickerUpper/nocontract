package gleam

import (
	"fmt"
	"strings"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/bindings"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/gleam/directives"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/nocontract/internal/sequence"
	"github.com/CheckPickerUpper/nocontract/messages"
	"github.com/CheckPickerUpper/outcome"
)

func (index projectIndex) readRoute(source sourceRoute) outcome.Result[routeRead] {
	return outcome.Fold(readLifetimeInput(source), func(lifetime bindings.RequestLifetimeInput) outcome.Result[routeRead] {
		return outcome.Fold(index.readInput(source.signature.Parameters[0].Type), func(input bindings.Record) outcome.Result[routeRead] {
			return outcome.Fold(index.readOutcome(source.signature.Returning, []compiler.Identity{}), func(output bindings.Outcome) outcome.Result[routeRead] {
				return outcome.Fold(index.readGuard(source), func(guard outcome.Option[bindings.Guard]) outcome.Result[routeRead] {
					return describedRoute(source, input, output, guard, lifetime)
				}, outcome.Err[routeRead])
			}, outcome.Err[routeRead])
		}, outcome.Err[routeRead])
	}, outcome.Err[routeRead])
}

func readLifetimeInput(source sourceRoute) outcome.Result[bindings.RequestLifetimeInput] {
	switch len(source.signature.Parameters) {
	case 1:
		return outcome.Ok[bindings.RequestLifetimeInput](bindings.WithoutRequestLifetime{})
	case 2:
		lifetime := compiler.Named{Identity: compiler.Identity{Package: "nocontract_runtime", Module: "nocontract_runtime/request_deadline", Name: "RequestLifetime"}, Arguments: []compiler.Type{}}
		if compiler.Equal(source.signature.Parameters[1].Type, lifetime) {
			return outcome.Ok[bindings.RequestLifetimeInput](bindings.WithRequestLifetime{})
		}
	}
	return outcome.Err[bindings.RequestLifetimeInput](DirectiveRefused{Declaration: source.route.Declaration, Requirement: "one record input, optionally followed by nocontract_runtime/request_deadline.RequestLifetime"})
}

func (index projectIndex) readInput(written compiler.Type) outcome.Result[bindings.Record] {
	named, isNamed := written.(compiler.Named)
	if !isNamed {
		return outcome.Err[bindings.Record](Unsupported{Type: written, Position: typePosition(index, written), Requirement: "one named public record input"})
	}
	return outcome.Fold(index.definition(named), func(definition compiler.Definition) outcome.Result[bindings.Record] {
		if len(definition.Constructors) != 1 {
			return outcome.Err[bindings.Record](Unsupported{Type: written, Position: typePosition(index, written), Requirement: "one public labelled record constructor"})
		}
		constructor := definition.Constructors[0]
		return outcome.Fold(index.readFields(constructor.Parameters, nil), func(fields []bindings.Field) outcome.Result[bindings.Record] {
			return outcome.Ok(bindings.Record{Native: named, Constructor: constructor, Fields: fields})
		}, outcome.Err[bindings.Record])
	}, outcome.Err[bindings.Record])
}

func describedRoute(source sourceRoute, input bindings.Record, output bindings.Outcome, guard outcome.Option[bindings.Guard], lifetime bindings.RequestLifetimeInput) outcome.Result[routeRead] {
	return outcome.Fold(readHeaders(source, input), func(headers []assembling.DeclaredHeader) outcome.Result[routeRead] {
		return describeRouteInput(source, input, output, guard, headers, lifetime)
	}, outcome.Err[routeRead])
}

// HeaderDeclarationRefused keeps the written binding and the typed declaration cause.
type HeaderDeclarationRefused struct {
	Header  directives.Header
	Problem assembling.Problem
}

// Error locates the header binding and reports its permanent declaration code.
func (failure HeaderDeclarationRefused) Error() string {
	return fmt.Sprintf("%s: %s: %s", failure.Header.Position, failure.Problem.ErrorCode(), failure.Problem.Report(messages.Match("")).Message)
}

func readHeaders(source sourceRoute, input bindings.Record) outcome.Result[[]assembling.DeclaredHeader] {
	fields, available := map[string]bindings.Field{}, []string{}
	for _, field := range input.Fields {
		fields[field.Name.Text()] = field
		available = append(available, field.Name.Text())
	}
	return sequence.Traverse(source.route.Headers, func(header directives.Header) outcome.Result[assembling.DeclaredHeader] {
		return outcome.FoldOption(keyed.Entry(fields, header.Field), func(field bindings.Field) outcome.Result[assembling.DeclaredHeader] {
			if header.Field == "body" {
				return outcome.Err[assembling.DeclaredHeader](HeaderDeclarationRefused{Header: header, Problem: assembling.HeaderBindingConflict{
					Name: header.Name, Field: header.Field, Part: "body", Binding: "body",
				}})
			}
			for _, segment := range source.route.Path.Segments() {
				switch segment := segment.(type) {
				case description.RouteParameter:
					if segment.Name() == header.Field {
						return outcome.Err[assembling.DeclaredHeader](HeaderDeclarationRefused{Header: header, Problem: assembling.HeaderBindingConflict{
							Name: header.Name, Field: header.Field, Part: "path", Binding: segment.Name(),
						}})
					}
				case description.RouteText, nil:
				}
			}
			return outcome.Ok(assembling.DeclaredHeader{Name: header.Name, Field: description.Field{Name: field.Name, Schema: field.Value.Schema()}, Position: header.Position})
		}, func() outcome.Result[assembling.DeclaredHeader] {
			return outcome.Err[assembling.DeclaredHeader](HeaderDeclarationRefused{Header: header, Problem: assembling.HeaderFieldUndeclared{
				Name: header.Name, Field: header.Field, Available: strings.Join(available, ", "),
			}})
		})
	})
}

func describeRouteInput(source sourceRoute, input bindings.Record, output bindings.Outcome, guard outcome.Option[bindings.Guard], headers []assembling.DeclaredHeader, lifetime bindings.RequestLifetimeInput) outcome.Result[routeRead] {
	parameters, query := []description.Field{}, []description.Field{}
	body := description.Body(description.NoBody{})
	pathNames := map[string]struct{}{}
	headerFields := map[string]struct{}{}
	for _, header := range headers {
		headerFields[header.Field.Name.Text()] = struct{}{}
	}
	for _, segment := range source.route.Path.Segments() {
		if parameter, isParameter := segment.(description.RouteParameter); isParameter {
			pathNames[parameter.Name()] = struct{}{}
		}
	}
	for _, field := range input.Fields {
		if _, header := headerFields[field.Name.Text()]; header {
			continue
		}
		described := description.Field{Name: field.Name, Schema: field.Value.Schema()}
		if field.Name.Text() == "body" {
			body = description.JSONBody{Schema: field.Value.Schema()}
			continue
		}
		if _, parameter := pathNames[field.Name.Text()]; parameter {
			parameters = append(parameters, described)
			continue
		}
		query = append(query, described)
	}
	successes, failures := []description.Member{}, []description.Member{}
	for _, member := range output.Members() {
		switch member.Answer.(type) {
		case description.Succeeds:
			successes = append(successes, member)
		case description.Fails:
			failures = append(failures, member)
		case nil:
		}
	}
	if len(successes) == 0 {
		return outcome.Err[routeRead](DirectiveRefused{Declaration: source.route.Declaration, Requirement: "at least one successful route outcome"})
	}
	outcome.ForEachOption(guard, func(guard bindings.Guard) { failures = append(failures, guard.Output.Members()...) })
	endpoint := description.EndpointName(publicName(source.name))
	return outcome.Ok(routeRead{declared: assembling.DeclaredEndpoint{Name: endpoint, Method: source.route.Method, Path: source.route.Path, Params: parameters, Query: query, Headers: headers, Body: body, Successes: description.Successes{First: successes[0], Rest: successes[1:]}, Failures: failures, Position: source.route.Declaration.Position}, native: bindings.Route{Module: source.module, Function: source.name, Group: routeGroup(source.module), Endpoint: endpoint, Input: input, Output: output, Lifetime: lifetime}, guard: guard})
}

func (index projectIndex) readGuard(source sourceRoute) outcome.Result[outcome.Option[bindings.Guard]] {
	return outcome.FoldOption(source.route.Guard, func(guard directives.Guard) outcome.Result[outcome.Option[bindings.Guard]] {
		return outcome.FoldOption(keyed.Entry(index.project.Modules, guard.Module), func(module compiler.Module) outcome.Result[outcome.Option[bindings.Guard]] {
			return outcome.FoldOption(keyed.Entry(module.Functions, guard.Function), func(function compiler.Function) outcome.Result[outcome.Option[bindings.Guard]] {
				return index.guardSignature(source, guard, function)
			}, func() outcome.Result[outcome.Option[bindings.Guard]] {
				return outcome.Err[outcome.Option[bindings.Guard]](DirectiveRefused{Declaration: source.route.Declaration, Requirement: "a public compiler-exported guard function"})
			})
		}, func() outcome.Result[outcome.Option[bindings.Guard]] {
			return outcome.Err[outcome.Option[bindings.Guard]](DirectiveRefused{Declaration: source.route.Declaration, Requirement: "a compiler-exported guard module"})
		})
	}, func() outcome.Result[outcome.Option[bindings.Guard]] {
		return outcome.Ok(outcome.None[bindings.Guard]())
	})
}

func (index projectIndex) guardSignature(source sourceRoute, guard directives.Guard, function compiler.Function) outcome.Result[outcome.Option[bindings.Guard]] {
	text := compiler.Named{Identity: compiler.Identity{Package: "", Module: "gleam", Name: "String"}, Arguments: []compiler.Type{}}
	headers := compiler.Named{Identity: compiler.Identity{Package: "gleam_stdlib", Module: "gleam/dict", Name: "Dict"}, Arguments: []compiler.Type{text, text}}
	returning, isNamed := function.Returning.(compiler.Named)
	if len(function.Parameters) != 2 || !compiler.Equal(function.Parameters[0].Type, text) || !compiler.Equal(function.Parameters[1].Type, headers) || !isNamed || returning.Identity != (compiler.Identity{Package: "gleam_stdlib", Module: "gleam/option", Name: "Option"}) || len(returning.Arguments) != 1 {
		return outcome.Err[outcome.Option[bindings.Guard]](Unsupported{Type: function.Returning, Position: source.route.Declaration.Position, Requirement: "guard (String, Dict(String, String)) -> Option(named refusal)"})
	}
	return outcome.Fold(index.readOutcome(returning.Arguments[0], []compiler.Identity{}), func(output bindings.Outcome) outcome.Result[outcome.Option[bindings.Guard]] {
		for _, member := range output.Members() {
			if _, success := member.Answer.(description.Succeeds); success {
				return outcome.Err[outcome.Option[bindings.Guard]](DirectiveRefused{Declaration: source.route.Declaration, Requirement: "only failure constructors in a guard refusal"})
			}
		}
		return outcome.Ok(outcome.Some(bindings.Guard{Module: guard.Module, Function: guard.Function, Scope: guard.Scope, Output: output}))
	}, outcome.Err[outcome.Option[bindings.Guard]])
}
