package reading

import (
	"cmp"
	"go/types"
	"maps"
	"path/filepath"
	"slices"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

// assembledPlan is everything the routes read found: the API they declare, which package
// assembling checks across routes, and the Go server plan, or every Go declaration refused.
func assembledPlan(index projectIndex, earlier found, guards guardTable, routes []reading[routeEntry]) Reading {
	entries, finding := []routeEntry{}, earlier.with(unknownDirectiveLines(index))
	for _, route := range routes {
		finding = finding.with(route.found)
		route.value.ForEach(func(entry routeEntry) { entries = append(entries, entry) })
	}
	byPackage := routesByPackage(entries)
	for _, group := range byPackage {
		finding = finding.with(generatedNameChecks(index, group))
	}
	finding = finding.with(serverNameChecks(index))
	read := keyed.FirstOfEach(finding.packages, func(path string) string { return path })
	slices.Sort(read)
	declared := declaredAPI(index, finding, byPackage)
	if len(finding.refusals) > 0 {
		return Reading{Packages: read, Routes: RoutesRefused{Declared: declared, Refusals: finding.refusals}}
	}
	return Reading{Packages: read, Routes: RoutesRead{Declared: declared, Plan: goPlan(index, guards, byPackage)}}
}

// unknownDirectiveLines refuses every //nocontract: line of the project's own files that names no
// directive NoContract reads.
func unknownDirectiveLines(index projectIndex) found {
	finding := nothingFound()
	for _, own := range index.own {
		for _, line := range unknownDirectives(own.file) {
			finding = finding.with(index.refused(line.position, DirectiveUnknown{Text: line.text}))
		}
	}
	return finding
}

// packageRoutes is the routes one package declares, in reading order.
type packageRoutes struct {
	loaded  *packages.Package
	entries []routeEntry
}

// routesByPackage is every route sorted into the package declaring it, the packages in import path
// order.
func routesByPackage(entries []routeEntry) []packageRoutes {
	grouped := map[string]packageRoutes{}
	for _, entry := range entries {
		group := outcome.FoldOption(keyed.Entry(grouped, entry.loaded.PkgPath), func(group packageRoutes) packageRoutes { return group },
			func() packageRoutes { return packageRoutes{loaded: entry.loaded, entries: []routeEntry{}} })
		grouped[entry.loaded.PkgPath] = packageRoutes{loaded: group.loaded, entries: append(slices.Clone(group.entries), entry)}
	}
	groups := []packageRoutes{}
	for _, path := range slices.Sorted(maps.Keys(grouped)) {
		groups = append(groups, grouped[path])
	}
	return groups
}

// declaredAPI is the API the routes read declare: each package's routes as one group, and every
// member and named struct once, in the order found.
func declaredAPI(index projectIndex, finding found, groups []packageRoutes) assembling.Declared {
	members := []assembling.DeclaredMember{}
	for _, member := range keyed.FirstOfEach(finding.members, func(member foundMember) typeKey { return member.key }) {
		members = append(members, assembling.DeclaredMember{Member: member.member, Position: member.position})
	}
	structs := []assembling.DeclaredStruct{}
	for _, named := range keyed.FirstOfEach(finding.structs, func(named foundStruct) typeKey { return named.key }) {
		structs = append(structs, assembling.DeclaredStruct{Struct: named.named, Position: named.position})
	}
	declaredGroups := []assembling.DeclaredGroup{}
	for _, group := range groups {
		endpoints := []assembling.DeclaredEndpoint{}
		for _, entry := range group.entries {
			endpoints = append(endpoints, entry.endpoint)
		}
		declaredGroups = append(declaredGroups, assembling.DeclaredGroup{Name: group.loaded.Name, Position: endpoints[0].Position, Endpoints: endpoints})
	}
	return assembling.Declared{API: index.request.API, Groups: declaredGroups, Members: members, Structs: structs}
}

// routeNeeds is everything one route of the package at home is served with: the receiver of its
// method handler and the guard it names, each when it has one.
func routeNeeds(route serverplan.Route, home string) []serverplan.RouteNeed {
	needs := []serverplan.RouteNeed{}
	switch handler := route.Handler.(type) {
	case serverplan.MethodHandler:
		needs = append(needs, serverplan.NeedsReceiver{Type: serverplan.GoName{Package: home, Name: handler.Receiver}})
	case serverplan.FunctionHandler, nil:
	}
	switch guard := route.Guard.(type) {
	case serverplan.Guarded:
		needs = append(needs, serverplan.NeedsGuard{Guard: guard.Guard})
	case serverplan.Unguarded, nil:
	}
	return needs
}

// generatedNameChecks refuses each route whose generated function's name the package already
// declares.
func generatedNameChecks(index projectIndex, group packageRoutes) found {
	finding := nothingFound()
	for _, entry := range group.entries {
		name := serverplan.RouteFunction(entry.route.Name)
		outcome.ForEachOption(outcome.EmptyableToOption(group.loaded.Types.Scope().Lookup(name)), func(types.Object) {
			finding = finding.with(index.refused(entry.position, GeneratedNameTaken{Name: name}))
		})
	}
	return finding
}

// plannedPackage is one package's generated code as it is planned: its package and directory, and
// what it serves and declares.
type plannedPackage struct {
	loaded    *packages.Package
	directory string
	serves    serverplan.Routing
	answers   []serverplan.SealedAnswer
}

// goPlan is the generated code of every package that declares a route or a guard's refusal, in
// import path order.
func goPlan(index projectIndex, guards guardTable, groups []packageRoutes) serverplan.Plan {
	planned := map[string]plannedPackage{}
	for _, group := range groups {
		planned[group.loaded.PkgPath] = plannedPackage{
			loaded: group.loaded, directory: filepath.Dir(index.position(group.entries[0].position).Filename),
			serves: servedRoutes(group), answers: []serverplan.SealedAnswer{},
		}
	}
	for _, name := range slices.Sorted(maps.Keys(guards.guards)) {
		guard := guards.guards[name]
		guard.members.ForEach(func(members []sealedMember) {
			refusal := serverplan.SealedAnswer{Use: serverplan.GuardAnswers{Guard: name}, Outcome: goName(guard.declared), Members: memberPlans(members)}
			path := guard.declared.loaded.PkgPath
			declaring := outcome.FoldOption(keyed.Entry(planned, path), func(declaring plannedPackage) plannedPackage { return declaring }, func() plannedPackage {
				return plannedPackage{
					loaded: guard.declared.loaded, directory: filepath.Dir(index.position(guard.declared.spec.Pos()).Filename),
					serves: serverplan.ServesNoRoutes{}, answers: []serverplan.SealedAnswer{},
				}
			})
			planned[path] = plannedPackage{
				loaded: declaring.loaded, directory: declaring.directory, serves: declaring.serves,
				answers: append(slices.Clone(declaring.answers), refusal),
			}
		})
	}
	packagesPlanned := []serverplan.Package{}
	for _, path := range slices.Sorted(maps.Keys(planned)) {
		each := planned[path]
		packagesPlanned = append(packagesPlanned, serverplan.Package{
			Name: each.loaded.Name, Path: path, Directory: each.directory, Serves: each.serves, Answers: each.answers,
		})
	}
	return serverplan.Plan{Packages: packagesPlanned, Server: plannedServer(index)}
}

// memberPlans is how the generated code answers each member.
func memberPlans(members []sealedMember) []serverplan.Member {
	plans := []serverplan.Member{}
	for _, member := range members {
		plans = append(plans, memberPlan(member))
	}
	return plans
}

// servedRoutes is one package's routes by name, what they are served with in the order first
// needed, and each outcome they answer once.
func servedRoutes(group packageRoutes) serverplan.ServesRoutes {
	routes, needs, outcomes := []serverplan.Route{}, []serverplan.RouteNeed{}, []serverplan.OutcomeAnswer{}
	for _, entry := range group.entries {
		routes = append(routes, entry.route)
		needs = append(needs, routeNeeds(entry.route, group.loaded.PkgPath)...)
		outcomes = append(outcomes, entry.outcome)
	}
	slices.SortFunc(routes, func(left, right serverplan.Route) int { return cmp.Compare(left.Name, right.Name) })
	return serverplan.ServesRoutes{
		Routes: routes, Needs: keyed.FirstOfEach(needs, func(need serverplan.RouteNeed) serverplan.RouteNeed { return need }),
		Outcomes: keyed.FirstOfEach(outcomes, func(answered serverplan.OutcomeAnswer) serverplan.GoName { return answered.Outcome }),
	}
}
