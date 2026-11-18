package writing

import (
	"cmp"
	"fmt"
	"go/format"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/nocontract/messages"
)

// RouteNotDescribed is a route of the server plan the API's description holds no endpoint for, so
// its path's segments are not known.
//
//ergo:code NC-1071
//ergo:message The route {Route} of the package {Package} has no endpoint in the API's description.
type RouteNotDescribed struct {
	Package string
	Route   string
}

// Error is the failure's message in the source language.
func (missing RouteNotDescribed) Error() string { return missing.Report(messages.Match("")).Message }

// endpointKey names an endpoint of the description by its group and its name.
type endpointKey struct {
	group string
	name  string
}

// describedPaths is each endpoint's path segments, by its group and its name.
func describedPaths(api description.API) map[endpointKey][]description.PathSegment {
	paths := map[endpointKey][]description.PathSegment{}
	for _, group := range api.Groups {
		for _, endpoint := range group.Endpoints {
			paths[endpointKey{group: group.Name, name: endpoint.Name}] = endpoint.Path
		}
	}
	return paths
}

// RenderServer is the formatted Go file of the server package: the API type, the only value that
// serves the plan's routes, and New, the only function that builds one. Each route's path is
// written from the segments the API's description holds for it.
func RenderServer(plan serverplan.Plan, api description.API) outcome.Result[[]byte] {
	names := serverImports(plan.Server.Home)
	routed := routePackages(plan.Packages)
	// Every package the file names takes its alias before any parameter takes a name.
	for _, each := range routed {
		names.reference(each.served.Path)
		for _, need := range each.routes.Needs {
			switch needed := need.(type) {
			case serverplan.NeedsGuard:
				names.reference(needed.Guard.Refusal.Package)
			case serverplan.NeedsReceiver, nil:
			}
		}
	}
	taken := slices.Concat(slices.Collect(maps.Values(names.aliases)), []string{"api", "layers", "request", "writer"}, serverplan.ServerNames())
	locals := &serverLocals{taken: taken}
	parameters := serverParameters(routed, names, locals)
	body := &strings.Builder{}
	fmt.Fprintf(body, `
// %[1]s serves every route the project declares, and answers every other request with
// RouteNotFound, MethodNotDeclared, or 204 and Allow to an OPTIONS request. Only %[2]s builds one.
type %[1]s struct {
	endpoints []%[3]s
	layers    []%[4]s
}

// ServeHTTP answers one request through the layers.
func (api %[1]s) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	%[5]s(writer, request, api.endpoints, api.layers)
}
`, serverplan.ServerType, serverplan.ServerFunction, names.runtime("Endpoint"), names.runtime("Layer"), names.runtime("Respond"))
	return outcome.Fold(routePaths(routed, describedPaths(api), names), func(paths map[routeKey]string) outcome.Result[[]byte] {
		body.WriteString(newFunctionText(routed, parameters, paths, names, locals))
		file := description.GeneratedHeader + "\npackage " + plan.Server.Name + "\n\n" + names.declaration() + "\n" + body.String()
		return outcome.TupleToResult(format.Source([]byte(file)))
	}, outcome.Err[[]byte])
}

// routeKey names a route by its package's import path and its endpoint name.
type routeKey struct {
	path string
	name string
}

// routePaths is each route's path as the server file writes it, a list of the runtime's path
// segments, or the first route the description holds no endpoint for.
func routePaths(routed []routedPackage, described map[endpointKey][]description.PathSegment, names *imports) outcome.Result[map[routeKey]string] {
	paths := outcome.Ok(map[routeKey]string{})
	for _, each := range routed {
		for _, route := range each.routes.Routes {
			paths = outcome.Fold(paths, func(written map[routeKey]string) outcome.Result[map[routeKey]string] {
				return outcome.FoldOption(keyed.Entry(described, endpointKey{group: each.served.Name, name: route.Name}),
					func(segments []description.PathSegment) outcome.Result[map[routeKey]string] {
						written[routeKey{path: each.served.Path, name: route.Name}] = segmentsText(segments, names)
						return outcome.Ok(written)
					}, func() outcome.Result[map[routeKey]string] {
						return outcome.Err[map[routeKey]string](RouteNotDescribed{Package: each.served.Path, Route: route.Name})
					})
			}, outcome.Err[map[routeKey]string])
		}
	}
	return paths
}

// segmentsText is a path's segments as the server file writes them: a list of the runtime's
// PathText and PathParameter.
func segmentsText(segments []description.PathSegment, names *imports) string {
	written := []string{}
	for _, segment := range segments {
		switch each := segment.(type) {
		case description.PathText:
			written = append(written, fmt.Sprintf("%s{Text: %s}", names.runtime("PathText"), strconv.Quote(each.Text)))
		case description.PathParameter:
			written = append(written, fmt.Sprintf("%s{Name: %s}", names.runtime("PathParameter"), strconv.Quote(each.Field.Name)))
		case nil:
		}
	}
	return "[]" + names.runtime("PathSegment") + "{" + strings.Join(written, ", ") + "}"
}

// serverImports names the packages the server file imports: the routes of a project package in the
// server folder by their bare names, with no alias taking a name that package declares.
func serverImports(home serverplan.ServerHome) *imports {
	fixed := [][2]string{{"net/http", "http"}, {"slices", "slices"}, {"github.com/samber/mo", "mo"}, {serverplan.NoContractPath, serverplan.NoContractPackage}}
	switch beside := home.(type) {
	case serverplan.ServerBesideProjectCode:
		return newImports(beside.Path, fixed, beside.Declared)
	case serverplan.ServerAlone, nil:
	}
	// No package has an empty import path, so every package the file names is imported.
	return newImports("", fixed, []string{})
}

// routedPackage is a package that declares routes and its routes.
type routedPackage struct {
	served serverplan.Package
	routes serverplan.ServesRoutes
}

// routePackages is every package that declares routes, in the order of their folders.
func routePackages(planned []serverplan.Package) []routedPackage {
	routed := []routedPackage{}
	for _, each := range planned {
		switch serves := each.Serves.(type) {
		case serverplan.ServesRoutes:
			routed = append(routed, routedPackage{served: each, routes: serves})
		case serverplan.ServesNoRoutes, nil:
		}
	}
	slices.SortFunc(routed, func(left, right routedPackage) int { return cmp.Compare(left.served.Directory, right.served.Directory) })
	return routed
}

// serverLocals are the names the server file's New takes, each free of every import alias and every
// name taken before it.
type serverLocals struct {
	taken []string
}

// named is the first free name of base, base itself or with a number after it.
func (locals *serverLocals) named(base string) string {
	name := base
	for suffix := 2; slices.Contains(locals.taken, name); suffix++ {
		name = base + strconv.Itoa(suffix)
	}
	locals.taken = append(locals.taken, name)
	return name
}

// packageNeed is one thing one package's routes are served with.
type packageNeed struct {
	path string
	need serverplan.RouteNeed
}

// serverParameter is one parameter of New: its name and its type as the file writes it.
type serverParameter struct {
	name     string
	typeText string
}

// serverParameters is every parameter of New after the layers, by package folder and then in the
// order the package's routes first need it, each named for the package and the receiver type or
// guard it holds.
func serverParameters(routed []routedPackage, names *imports, locals *serverLocals) map[packageNeed]serverParameter {
	parameters := map[packageNeed]serverParameter{}
	for _, each := range routed {
		packageName := each.served.Name
		switch reference := names.reference(each.served.Path).(type) {
		case qualifiedBy:
			packageName = reference.alias
		case unqualified, nil:
		}
		for _, need := range each.routes.Needs {
			switch needed := need.(type) {
			case serverplan.NeedsReceiver:
				parameters[packageNeed{path: each.served.Path, need: need}] = serverParameter{
					name: locals.named(packageName + needed.Type.Name), typeText: names.qualified(needed.Type),
				}
			case serverplan.NeedsGuard:
				parameters[packageNeed{path: each.served.Path, need: need}] = serverParameter{
					name:     locals.named(packageName + needed.Guard.Name),
					typeText: names.runtime("Guard") + "[" + names.qualified(needed.Guard.Refusal) + "]",
				}
			case nil:
			}
		}
	}
	return parameters
}

// guardCheck is how one guarded route's guard decides its requests: the parameter holding the
// guard, the route it is asked to check, and the name its decision is bound to.
type guardCheck struct {
	guard    serverParameter
	route    serverplan.Route
	argument string
	refusal  serverplan.GoName
	name     string
}

// newFunctionText is New: each guard asked to check its route, then the API holding every route
// behind its guard's check.
func newFunctionText(routed []routedPackage, parameters map[packageNeed]serverParameter, paths map[routeKey]string, names *imports, locals *serverLocals) string {
	text := &strings.Builder{}
	signature := []string{"layers []" + names.runtime("Layer")}
	for _, each := range routed {
		for _, need := range each.routes.Needs {
			parameter := parameters[packageNeed{path: each.served.Path, need: need}]
			signature = append(signature, parameter.name+" "+parameter.typeText)
		}
	}
	fmt.Fprintf(text, "\n// %[1]s is the %[2]s serving every route the project declares, each behind the guard it names, with\n"+
		"// the layers run around every request, the first outermost; or why a guard cannot check a route.\n"+
		"// It takes the receiver of each method handler and each guard, by the package declaring the routes.\n"+
		"func %[1]s(%[3]s) mo.Result[%[2]s] {\n", serverplan.ServerFunction, serverplan.ServerType, strings.Join(signature, ", "))
	checks, endpoints := []guardCheck{}, []string{}
	for _, each := range routed {
		for _, route := range each.routes.Routes {
			call := routeCall(each.served.Path, route, parameters, names)
			switch guard := route.Guard.(type) {
			case serverplan.Guarded:
				check := guardCheck{
					guard: parameters[packageNeed{path: each.served.Path, need: serverplan.NeedsGuard{Guard: guard.Guard}}], route: route,
					argument: guard.Argument, refusal: guard.Guard.Refusal, name: locals.named("check" + exported(route.Name)),
				}
				checks = append(checks, check)
				refused := names.qualified(serverplan.GoName{Package: guard.Guard.Refusal.Package, Name: serverplan.AnswerPrefix + guard.Guard.Refusal.Name})
				call = fmt.Sprintf("return %s(%s(request), %s, func() %s { %s })", names.runtime("Decided"), check.name, refused, names.runtime("Answer"), call)
			case serverplan.Unguarded, nil:
			}
			endpoints = append(endpoints, fmt.Sprintf("{Method: %s, Path: %s, Answer: func(request *http.Request) %s { %s }},",
				strconv.Quote(route.Method.Name()), paths[routeKey{path: each.served.Path, name: route.Name}], names.runtime("Answer"), call))
		}
	}
	for _, check := range checks {
		fmt.Fprintf(text, "return mo.Fold(%s.CheckRoute(%s{Method: %s, Path: %s, Argument: %s}), func(%s %s[%s]) mo.Result[%s] {\n",
			check.guard.name, names.runtime("GuardedRoute"), strconv.Quote(check.route.Method.Name()), strconv.Quote(check.route.Path),
			strconv.Quote(check.argument), check.name, names.runtime("RequestCheck"), names.qualified(check.refusal), serverplan.ServerType)
	}
	fmt.Fprintf(text, "return mo.Ok(%s{layers: slices.Clone(layers), endpoints: []%s{\n%s\n}})\n", serverplan.ServerType, names.runtime("Endpoint"), strings.Join(endpoints, "\n"))
	text.WriteString(strings.Repeat(fmt.Sprintf("}, mo.Err[%s])\n", serverplan.ServerType), len(checks)))
	text.WriteString("}\n")
	return text.String()
}

// routeCall is the statement answering a request with a route's generated function.
func routeCall(path string, route serverplan.Route, parameters map[packageNeed]serverParameter, names *imports) string {
	function := names.qualified(serverplan.GoName{Package: path, Name: serverplan.RouteFunction(route.Name)})
	switch handler := route.Handler.(type) {
	case serverplan.MethodHandler:
		receiver := parameters[packageNeed{path: path, need: serverplan.NeedsReceiver{Type: serverplan.GoName{Package: path, Name: handler.Receiver}}}]
		return "return " + function + "(" + receiver.name + ", request)"
	case serverplan.FunctionHandler, nil:
	}
	return "return " + function + "(request)"
}
