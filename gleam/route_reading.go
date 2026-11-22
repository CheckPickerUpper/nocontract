package gleam

import (
	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/bindings"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/gleam/directives"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/outcome"
)

func (index projectIndex) readRoute(source sourceRoute) outcome.Result[routeRead] {
	if len(source.signature.Parameters) != 1 {
		return outcome.Err[routeRead](DirectiveRefused{Declaration: source.route.Declaration, Requirement: "one record parameter on a route function"})
	}
	return outcome.Fold(index.readInput(source.signature.Parameters[0].Type), func(input bindings.Record) outcome.Result[routeRead] {
		return outcome.Fold(index.readOutcome(source.signature.Returning, []compiler.Identity{}), func(output bindings.Outcome) outcome.Result[routeRead] {
			return outcome.Fold(index.readGuard(source), func(guard outcome.Option[guardRead]) outcome.Result[routeRead] {
				return describedRoute(source, input, output, guard)
			}, outcome.Err[routeRead])
		}, outcome.Err[routeRead])
	}, outcome.Err[routeRead])
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

func describedRoute(source sourceRoute, input bindings.Record, output bindings.Outcome, guard outcome.Option[guardRead]) outcome.Result[routeRead] {
	parameters, query := []description.Field{}, []description.Field{}
	body := description.Body(description.NoBody{})
	pathNames := map[string]struct{}{}
	for _, segment := range source.route.Path.Segments() {
		if parameter, isParameter := segment.(description.RouteParameter); isParameter {
			pathNames[parameter.Name()] = struct{}{}
		}
	}
	for _, field := range input.Fields {
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
	outcome.ForEachOption(guard, func(guard guardRead) { failures = append(failures, guard.route.Refusals...) })
	endpoint := description.EndpointName(publicName(source.name))
	return outcome.Ok(routeRead{declared: assembling.DeclaredEndpoint{Name: endpoint, Method: source.route.Method, Path: source.route.Path, Params: parameters, Query: query, Body: body, Successes: description.Successes{First: successes[0], Rest: successes[1:]}, Failures: failures, Position: source.route.Declaration.Position}, native: bindings.Route{Module: source.module, Function: source.name, Group: routeGroup(source.module), Endpoint: endpoint, Input: input, Output: output}, guard: guard})
}

func (index projectIndex) readGuard(source sourceRoute) outcome.Result[outcome.Option[guardRead]] {
	return outcome.FoldOption(source.route.Guard, func(guard directives.Guard) outcome.Result[outcome.Option[guardRead]] {
		return outcome.FoldOption(keyed.Entry(index.project.Modules, guard.Module), func(module compiler.Module) outcome.Result[outcome.Option[guardRead]] {
			return outcome.FoldOption(keyed.Entry(module.Functions, guard.Function), func(function compiler.Function) outcome.Result[outcome.Option[guardRead]] {
				return index.guardSignature(source, guard, function)
			}, func() outcome.Result[outcome.Option[guardRead]] {
				return outcome.Err[outcome.Option[guardRead]](DirectiveRefused{Declaration: source.route.Declaration, Requirement: "a public compiler-exported guard function"})
			})
		}, func() outcome.Result[outcome.Option[guardRead]] {
			return outcome.Err[outcome.Option[guardRead]](DirectiveRefused{Declaration: source.route.Declaration, Requirement: "a compiler-exported guard module"})
		})
	}, func() outcome.Result[outcome.Option[guardRead]] { return outcome.Ok(outcome.None[guardRead]()) })
}

func (index projectIndex) guardSignature(source sourceRoute, guard directives.Guard, function compiler.Function) outcome.Result[outcome.Option[guardRead]] {
	text := compiler.Named{Identity: compiler.Identity{Package: "", Module: "gleam", Name: "String"}, Arguments: []compiler.Type{}}
	headers := compiler.Named{Identity: compiler.Identity{Package: "gleam_stdlib", Module: "gleam/dict", Name: "Dict"}, Arguments: []compiler.Type{text, text}}
	returning, isNamed := function.Returning.(compiler.Named)
	if len(function.Parameters) != 2 || !compiler.Equal(function.Parameters[0].Type, text) || !compiler.Equal(function.Parameters[1].Type, headers) || !isNamed || returning.Identity != (compiler.Identity{Package: "gleam_stdlib", Module: "gleam/option", Name: "Option"}) || len(returning.Arguments) != 1 {
		return outcome.Err[outcome.Option[guardRead]](Unsupported{Type: function.Returning, Position: source.route.Declaration.Position, Requirement: "guard (String, Dict(String, String)) -> Option(named refusal)"})
	}
	return outcome.Fold(index.readOutcome(returning.Arguments[0], []compiler.Identity{}), func(output bindings.Outcome) outcome.Result[outcome.Option[guardRead]] {
		for _, member := range output.Members() {
			if _, success := member.Answer.(description.Succeeds); success {
				return outcome.Err[outcome.Option[guardRead]](DirectiveRefused{Declaration: source.route.Declaration, Requirement: "only failure constructors in a guard refusal"})
			}
		}
		group, endpoint := routeGroup(source.module), description.EndpointName(publicName(source.name))
		return outcome.Ok(outcome.Some(guardRead{native: bindings.Guard{Module: guard.Module, Function: guard.Function, Scope: guard.Scope, Output: output}, route: native.GuardedRoute{Group: group, Endpoint: endpoint, Scope: guard.Scope, Refusals: output.Members()}}))
	}, outcome.Err[outcome.Option[guardRead]])
}
