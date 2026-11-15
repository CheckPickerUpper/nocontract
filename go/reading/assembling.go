package reading

import (
	"cmp"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"

	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// assemble runs the checks that span routes over everything found, and builds the description and
// the server plan when nothing was refused.
func assemble(index projectIndex, earlier found, guards guardTable, routes []reading[routeEntry]) Reading {
	entries, finding := []routeEntry{}, earlier.with(unknownDirectiveLines(index))
	for _, route := range routes {
		finding = finding.with(route.found)
		route.value.ForEach(func(entry routeEntry) { entries = append(entries, entry) })
	}
	byPackage := groupedByPackage(entries)
	finding = finding.with(repeatedPatterns(index, entries))
	for _, group := range byPackage {
		finding = finding.with(packageChecks(index, group))
	}
	finding = finding.with(repeatedNames(index, finding, byPackage))
	read := distinctBy(finding.packages, func(path string) string { return path })
	slices.Sort(read)
	refusals := sortedRefusals(finding)
	if len(refusals) > 0 {
		return Reading{Read: read, Routes: RoutesRefused{Refusals: refusals}}
	}
	return Reading{Read: read, Routes: RoutesRead{API: describe(index, finding, byPackage), Plan: planOf(index, guards, byPackage)}}
}

// sortedRefusals is every refusal found once, in source order: a member or struct several routes
// read is refused once.
func sortedRefusals(finding found) []Refusal {
	refusals := distinctBy(finding.refusals, func(refusal Refusal) Refusal { return refusal })
	slices.SortStableFunc(refusals, func(left, right Refusal) int {
		return cmp.Or(cmp.Compare(left.Position.Filename, right.Position.Filename), cmp.Compare(left.Position.Line, right.Position.Line))
	})
	return refusals
}

// distinctBy is every item once: the first of each key, in order.
func distinctBy[Item any, Key comparable](items []Item, key func(Item) Key) []Item {
	seen, kept := map[Key]struct{}{}, []Item{}
	for _, item := range items {
		if _, isSeen := seen[key(item)]; isSeen {
			continue
		}
		seen[key(item)] = struct{}{}
		kept = append(kept, item)
	}
	return kept
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

// groupedByPackage is every route sorted into the package declaring it, the packages in import
// path order.
func groupedByPackage(entries []routeEntry) []packageRoutes {
	grouped := map[string]packageRoutes{}
	for _, entry := range entries {
		group, known := grouped[entry.loaded.PkgPath]
		if !known {
			group = packageRoutes{loaded: entry.loaded, entries: []routeEntry{}}
		}
		grouped[entry.loaded.PkgPath] = packageRoutes{loaded: group.loaded, entries: append(slices.Clone(group.entries), entry)}
	}
	groups := []packageRoutes{}
	for _, path := range slices.Sorted(maps.Keys(grouped)) {
		groups = append(groups, grouped[path])
	}
	return groups
}

// repeatedPatterns refuses every route serving a method and path an earlier route serves.
func repeatedPatterns(index projectIndex, entries []routeEntry) found {
	finding, first := nothingFound(), map[string]token.Position{}
	for _, entry := range entries {
		pattern := entry.route.Method.Name() + " " + entry.route.Path
		if served, taken := first[pattern]; taken {
			finding = finding.with(index.refused(entry.position, RouteRepeated{Pattern: pattern, First: served}))
			continue
		}
		first[pattern] = index.position(entry.position)
	}
	return finding
}

// servingField is one field of what a package's routes are served with, named for the receiver
// type or the guard it holds.
//
//sumtype:decl
type servingField interface {
	isServingField()
	fieldName() string
}

// receiverField holds the receiver of the package's method handlers of one type.
//
//ergo:not-an-error
type receiverField struct{ name string }

// guardField holds one guard the package's routes name.
//
//ergo:not-an-error
type guardField struct{ name string }

func (receiverField) isServingField() {}
func (guardField) isServingField()    {}

func (field receiverField) fieldName() string { return field.name }
func (field guardField) fieldName() string    { return field.name }

// servingFieldsOf is every field one route needs in what its package is served with.
func servingFieldsOf(route serverplan.Route) []servingField {
	fields := []servingField{}
	switch handler := route.Handler.(type) {
	case serverplan.MethodHandler:
		fields = append(fields, receiverField{name: handler.Receiver})
	case serverplan.FunctionHandler, nil:
	}
	switch guard := route.Guard.(type) {
	case serverplan.Guarded:
		fields = append(fields, guardField{name: guard.Name})
	case serverplan.Unguarded, nil:
	}
	return fields
}

// packageChecks refuses, within one package, an endpoint name two routes take, a field of what the
// routes are served with that two kinds of field take, and a name the generated file declares that
// the package already declares.
func packageChecks(index projectIndex, group packageRoutes) found {
	finding, endpoints, fields := nothingFound(), map[string]token.Position{}, map[string]servingField{}
	for _, entry := range group.entries {
		if first, taken := endpoints[entry.endpoint.Name]; taken {
			finding = finding.with(index.refused(entry.position, EndpointNameRepeated{Name: entry.endpoint.Name, First: first}))
		}
		endpoints[entry.endpoint.Name] = index.position(entry.position)
		for _, field := range servingFieldsOf(entry.route) {
			if held, taken := fields[field.fieldName()]; taken && held != field {
				finding = finding.with(index.refused(entry.position, GeneratedNameTaken{Name: serverplan.ServingType + "." + field.fieldName()}))
				continue
			}
			fields[field.fieldName()] = field
		}
	}
	for _, name := range serverplan.CalledNames() {
		mo.EmptyableToOption(group.loaded.Types.Scope().Lookup(name)).ForEach(func(declared types.Object) {
			switch serverplan.SourceFileAt(index.position(declared.Pos()).Filename).(type) {
			case serverplan.WrittenFile:
				finding = finding.with(index.refused(group.entries[0].position, GeneratedNameTaken{Name: name}))
			case serverplan.GeneratedFile, nil:
			}
		})
	}
	return finding
}

// nameClaim is one declaration taking a name in the TypeScript file: the name, what takes it and
// where.
type nameClaim struct {
	name     string
	owner    typeKey
	position token.Position
}

// repeatedNames refuses every declaration taking a TypeScript name another declaration took: the
// names the file imports, the API class, every member and named struct, and each package's group
// class.
func repeatedNames(index projectIndex, finding found, groups []packageRoutes) found {
	claims := []nameClaim{}
	for _, name := range append(reservedNames(), index.request.APIName) {
		claims = append(claims, nameClaim{name: name, owner: typeKey{path: "", name: name}, position: index.position(token.NoPos)})
	}
	for _, member := range finding.members {
		claims = append(claims, nameClaim{name: member.member.Name, owner: member.key, position: member.position})
	}
	for _, named := range finding.structs {
		claims = append(claims, nameClaim{name: named.named.Name, owner: named.key, position: named.position})
	}
	for _, group := range groups {
		claims = append(claims, nameClaim{
			name: description.GroupClassName(group.loaded.Name), owner: typeKey{path: group.loaded.PkgPath, name: ""}, position: index.position(group.entries[0].position),
		})
	}
	refused, first := nothingFound(), map[string]nameClaim{}
	for _, claim := range claims {
		taken, isTaken := first[claim.name]
		switch {
		case !isTaken:
			first[claim.name] = claim
		case taken.owner != claim.owner:
			refused = refused.with(refusedAt(claim.position, NameRepeated{Name: claim.name, First: taken.position}))
		}
	}
	return refused
}

// describe is the language-neutral description of every route read: each member and named struct
// once, in the order found, and each package's endpoints by name.
func describe(index projectIndex, finding found, groups []packageRoutes) description.API {
	members := []description.Member{}
	for _, member := range distinctBy(finding.members, func(member foundMember) typeKey { return member.key }) {
		members = append(members, member.member)
	}
	structs := []description.NamedStruct{}
	for _, named := range distinctBy(finding.structs, func(named foundStruct) typeKey { return named.key }) {
		structs = append(structs, named.named)
	}
	described := []description.Group{}
	for _, group := range groups {
		endpoints := []description.Endpoint{}
		for _, entry := range group.entries {
			endpoints = append(endpoints, entry.endpoint)
		}
		slices.SortFunc(endpoints, func(left, right description.Endpoint) int { return cmp.Compare(left.Name, right.Name) })
		described = append(described, description.Group{Name: group.loaded.Name, Endpoints: endpoints})
	}
	slices.SortFunc(described, func(left, right description.Group) int { return cmp.Compare(left.Name, right.Name) })
	return description.API{Name: index.request.APIName, Groups: described, Members: members, Structs: structs}
}

// plannedPackage is one package's generated code as it is planned: its package and directory, and
// what it serves and declares.
type plannedPackage struct {
	loaded    *packages.Package
	directory string
	serves    serverplan.Routing
	answers   []serverplan.SealedAnswer
}

// planOf is the generated code of every package that declares a route or a guard's refusal, in
// import path order.
func planOf(index projectIndex, guards guardTable, groups []packageRoutes) serverplan.Plan {
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
			refusal := serverplan.SealedAnswer{Use: serverplan.GuardAnswers{Guard: name}, Outcome: goName(guard.declared), Members: plansOfMembers(members)}
			path := guard.declared.loaded.PkgPath
			declaring, known := planned[path]
			if !known {
				declaring = plannedPackage{
					loaded: guard.declared.loaded, directory: filepath.Dir(index.position(guard.declared.spec.Pos()).Filename),
					serves: serverplan.ServesNoRoutes{}, answers: []serverplan.SealedAnswer{},
				}
			}
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
	return serverplan.Plan{Packages: packagesPlanned}
}

func plansOfMembers(members []sealedMember) []serverplan.Member {
	plans := []serverplan.Member{}
	for _, member := range members {
		plans = append(plans, memberPlan(member))
	}
	return plans
}

// servedRoutes is one package's routes by name, the fields they are served with in the order first
// needed, and each outcome they answer once.
func servedRoutes(group packageRoutes) serverplan.ServesRoutes {
	routes, fields, outcomes := []serverplan.Route{}, []servingField{}, []serverplan.OutcomeAnswer{}
	for _, entry := range group.entries {
		routes = append(routes, entry.route)
		fields = append(fields, servingFieldsOf(entry.route)...)
		outcomes = append(outcomes, entry.outcome)
	}
	serving := serverplan.Serving{Receivers: []serverplan.GoName{}, Guards: []string{}}
	for _, field := range distinctBy(fields, func(field servingField) servingField { return field }) {
		switch needed := field.(type) {
		case receiverField:
			serving = serverplan.Serving{Receivers: append(slices.Clone(serving.Receivers), serverplan.GoName{Package: group.loaded.PkgPath, Name: needed.name}), Guards: serving.Guards}
		case guardField:
			serving = serverplan.Serving{Receivers: serving.Receivers, Guards: append(slices.Clone(serving.Guards), needed.name)}
		case nil:
		}
	}
	slices.SortFunc(routes, func(left, right serverplan.Route) int { return cmp.Compare(left.Name, right.Name) })
	return serverplan.ServesRoutes{
		Routes: routes, Serving: serving,
		Outcomes: distinctBy(outcomes, func(answered serverplan.OutcomeAnswer) serverplan.GoName { return answered.Outcome }),
	}
}
