package reading

import (
	"cmp"
	"go/types"
	"maps"
	"path/filepath"
	"slices"

	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// assembledPlan is everything the routes read found: the API they declare, which package
// assembling checks across routes, every Go declaration refused, and the Go server plan.
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
	read := distinctBy(finding.packages, func(path string) string { return path })
	slices.Sort(read)
	return Reading{Read: read, Declared: declaredAPI(index, finding, byPackage), Refused: finding.refusals, Plan: goPlan(index, guards, byPackage)}
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

// routesByPackage is every route sorted into the package declaring it, the packages in import path
// order.
func routesByPackage(entries []routeEntry) []packageRoutes {
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

// declaredAPI is the API the routes read declare: each package's routes as one group, and every
// member and named struct once, in the order found.
func declaredAPI(index projectIndex, finding found, groups []packageRoutes) assembling.Declared {
	members := []assembling.DeclaredMember{}
	for _, member := range distinctBy(finding.members, func(member foundMember) typeKey { return member.key }) {
		members = append(members, assembling.DeclaredMember{Member: member.member, Position: member.position})
	}
	structs := []assembling.DeclaredStruct{}
	for _, named := range distinctBy(finding.structs, func(named foundStruct) typeKey { return named.key }) {
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

// servingFieldsNeeded is every field one route needs in what its package is served with.
func servingFieldsNeeded(route serverplan.Route) []servingField {
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

// generatedNameChecks refuses, within one package, a field of what the routes are served with that
// two kinds of field take, and a name the generated file declares that the package already
// declares.
func generatedNameChecks(index projectIndex, group packageRoutes) found {
	finding, fields := nothingFound(), map[string]servingField{}
	for _, entry := range group.entries {
		for _, field := range servingFieldsNeeded(entry.route) {
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

// memberPlans is how the generated code answers each member.
func memberPlans(members []sealedMember) []serverplan.Member {
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
		fields = append(fields, servingFieldsNeeded(entry.route)...)
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
