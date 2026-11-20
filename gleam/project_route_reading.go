// Package gleam reads compiler-checked Gleam routes and generates their native bridge.
package gleam

import (
	"cmp"
	"context"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/authored"
	"github.com/CheckPickerUpper/nocontract/gleam/bindings"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/gleam/sequence"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/outcome"
)

// Read resolves public signatures with Gleam, reads route metadata, and prepares typed bindings.
func Read(context context.Context, request native.Request) outcome.Result[native.Project] {
	return outcome.Fold(compiler.Read(context, request.Directory), func(project compiler.Package) outcome.Result[native.Project] {
		return outcome.Fold(authored.Read(request.Directory, project), func(metadata map[string]authored.Module) outcome.Result[native.Project] {
			index := projectIndex{project: project, metadata: metadata, records: map[compiler.Identity]bindings.Record{}, outcomes: map[compiler.Identity]bindings.Outcome{}}
			return outcome.Fold(index.readRoutes(request), func(routes []routeRead) outcome.Result[native.Project] {
				return outcome.Ok(index.projectBindings(request, routes))
			}, outcome.Err[native.Project])
		}, outcome.Err[native.Project])
	}, outcome.Err[native.Project])
}

type sourceRoute struct {
	module      string
	name        string
	signature   compiler.Function
	declaration authored.Declaration
	route       authored.Directive
}
type routeRead struct {
	declared assembling.DeclaredEndpoint
	native   bindings.Route
	guard    outcome.Option[guardRead]
}
type guardRead struct {
	native bindings.Guard
	route  native.GuardedRoute
}

func (index projectIndex) readRoutes(request native.Request) outcome.Result[[]routeRead] {
	sources := []sourceRoute{}
	for _, moduleName := range slices.Sorted(maps.Keys(index.project.Modules)) {
		if !selectedModule(request.Packages, moduleName) {
			continue
		}
		module := index.project.Modules[moduleName]
		for _, name := range slices.Sorted(maps.Keys(module.Functions)) {
			metadata := index.metadata[moduleName]
			if declaration, declared := metadata.Functions[name]; declared {
				for _, directive := range declaration.Directives {
					if directive.Name == "route" {
						sources = append(sources, sourceRoute{module: moduleName, name: name, signature: module.Functions[name], declaration: declaration, route: directive})
					}
				}
			}
		}
	}
	if len(sources) == 0 {
		return outcome.Err[[]routeRead](DirectiveRefused{Declaration: authored.Declaration{Position: request.API.Position, Directives: []authored.Directive{}}, Requirement: "at least one selected public route"})
	}
	return sequence.Traverse(sources, index.readRoute)
}

func selectedModule(patterns []string, module string) bool {
	for _, pattern := range patterns {
		if pattern == "./..." || pattern == module {
			return true
		}
	}
	return false
}

func (index projectIndex) projectBindings(request native.Request, routes []routeRead) native.Project {
	groups := map[string]assembling.DeclaredGroup{}
	guards, nativeRoutes, nativeGuards := []native.GuardedRoute{}, []bindings.Route{}, []bindings.Guard{}
	for _, route := range routes {
		group := outcome.FoldOption(keyed.Entry(groups, route.native.Group), func(group assembling.DeclaredGroup) assembling.DeclaredGroup { return group }, func() assembling.DeclaredGroup {
			return assembling.DeclaredGroup{Name: route.native.Group, Position: route.declared.Position, Endpoints: []assembling.DeclaredEndpoint{}}
		})
		group.Endpoints = append(group.Endpoints, route.declared)
		groups[group.Name] = group
		nativeRoutes = append(nativeRoutes, route.native)
		outcome.ForEachOption(route.guard, func(guard guardRead) {
			guards = append(guards, guard.route)
			nativeGuards = append(nativeGuards, guard.native)
		})
	}
	declaredGroups := []assembling.DeclaredGroup{}
	for _, name := range slices.Sorted(maps.Keys(groups)) {
		declaredGroups = append(declaredGroups, groups[name])
	}
	members := []assembling.DeclaredMember{}
	for _, identity := range sortedIdentities(index.outcomes) {
		returned := index.outcomes[identity]
		for _, answer := range returned.Answers {
			if direct, isDirect := answer.(bindings.Direct); isDirect {
				members = append(members, assembling.DeclaredMember{Member: direct.Members()[0], Position: index.position(identity, direct.Constructor.Name)})
			}
		}
	}
	structs := []assembling.DeclaredStruct{}
	for _, identity := range sortedIdentities(index.records) {
		record := index.records[identity]
		structs = append(structs, assembling.DeclaredStruct{Struct: description.NamedStruct{Name: identity.Name, Fields: bindings.Fields(record.Fields)}, Position: index.position(identity, record.Constructor.Name)})
	}
	declared := assembling.Declared{API: request.API, Groups: declaredGroups, Members: members, Structs: structs}
	return native.Project{Declared: declared, Guards: guards, Generate: func(assembled assembling.APIAssembled) outcome.Result[[]native.File] {
		return bindings.Generate(bindings.Project{Directory: request.Directory, Server: request.Server, Package: index.project.Name, Routes: nativeRoutes, Guards: nativeGuards}, assembled)
	}, Command: native.Command{Directory: request.Server, Arguments: []string{"gleam", "run", "--no-print-progress", "-m", "nocontract_bridge"}}}
}

func sortedIdentities[T any](values map[compiler.Identity]T) []compiler.Identity {
	identities := slices.Collect(maps.Keys(values))
	slices.SortFunc(identities, func(left, right compiler.Identity) int {
		return cmp.Compare(left.Package+"/"+left.Module+"."+left.Name, right.Package+"/"+right.Module+"."+right.Name)
	})
	return identities
}

func publicName(name string) string {
	parts := strings.Split(name, "_")
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}

func routeGroup(module string) string { return path.Base(filepath.ToSlash(module)) }
