package reading

import (
	"cmp"
	"go/token"
	"slices"
	"strings"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// guardReading is a guard's refusal read once: its declaration and members, or refused.
type guardReading struct {
	declared declaredType
	members  mo.Option[[]readMember]
}

// guardName is text read as a guard's name: one exported Go identifier, the form it takes as a
// field of what routes are served with.
func guardName(text string) mo.Option[string] {
	if !token.IsIdentifier(text) || !token.IsExported(text) {
		return mo.None[string]()
	}
	return mo.Some(text)
}

// readGuards reads every sealed refusal marked //nocontract:guard in the project's packages, in
// declaration order, before any route names one.
func (reader *projectReader) readGuards() {
	declared := []declaredType{}
	for key, each := range reader.declared {
		if key.path != NoContractPath && len(named(directivesIn(each.comment), guardDirective)) > 0 {
			declared = append(declared, each)
		}
	}
	slices.SortFunc(declared, func(left, right declaredType) int {
		return cmp.Or(cmp.Compare(packagePath(left.object), packagePath(right.object)), cmp.Compare(left.object.Pos(), right.object.Pos()))
	})
	for _, each := range declared {
		reader.readGuard(each)
	}
}

func (reader *projectReader) readGuard(declared declaredType) {
	found := named(directivesIn(declared.comment), guardDirective)
	if len(found) > 1 {
		reader.refuse(found[1].position, GuardDirectiveMalformed{Text: found[1].argument})
		return
	}
	mo.Fold(guardName(found[0].argument), func(name string) struct{} {
		reader.readNamedGuard(declared, name)
		return struct{}{}
	}, func(error) struct{} {
		reader.refuse(found[0].position, GuardDirectiveMalformed{Text: found[0].argument})
		return struct{}{}
	})
}

func (reader *projectReader) readNamedGuard(declared declaredType, name string) {
	if first, known := reader.guards[name]; known {
		reader.refuse(declared.spec.Pos(), GuardRepeated{Guard: name, First: reader.files.Position(first.declared.spec.Pos())})
		return
	}
	members := thenOption(reader.readSealed(keyOf(declared.object), declared.spec.Pos()), func(sealed sealedRead) mo.Option[[]readMember] {
		failing := true
		for _, member := range sealed.members {
			if succeeds, isSuccess := member.answer.(description.Succeeds); isSuccess {
				reader.refuse(member.object.Pos(), GuardMemberSucceeds{Member: member.object.Name(), Status: succeeds.Status})
				failing = false
			}
		}
		if !failing {
			return mo.None[[]readMember]()
		}
		return mo.Some(sealed.members)
	})
	reader.guards[name] = guardReading{declared: declared, members: members}
	members.ForEach(func(read []readMember) {
		plans := []serverplan.Member{}
		for _, member := range read {
			plans = append(plans, memberPlan(member))
		}
		build := reader.buildFor(declared.loaded, declared.spec.Pos())
		build.plan.Refusals = append(build.plan.Refusals, serverplan.GuardRefusal{Guard: name, Outcome: goName(declared.object), Members: plans})
	})
}

// routeGuard is the guard a handler's //nocontract:guard line names, with the members of its
// refusal: none when the handler names no guard.
type routeGuard struct {
	plan    serverplan.RouteGuard
	members []string
}

// readRouteGuard reads a handler's //nocontract:guard line, naming a declared guard and the one
// argument it is handed.
func (reader *projectReader) readRouteGuard(found []directive) mo.Option[routeGuard] {
	if len(found) == 0 {
		return mo.Some(routeGuard{plan: serverplan.Unguarded{}, members: []string{}})
	}
	words := strings.Fields(found[0].argument)
	malformed := func() mo.Option[routeGuard] {
		reader.refuse(found[0].position, GuardDirectiveMalformed{Text: found[0].argument})
		return mo.None[routeGuard]()
	}
	if len(found) > 1 || len(words) != 2 {
		return malformed()
	}
	return mo.Fold(guardName(words[0]), func(name string) mo.Option[routeGuard] {
		guard, declared := reader.guards[name]
		if !declared {
			reader.refuse(found[0].position, GuardUndeclared{Guard: name})
			return mo.None[routeGuard]()
		}
		return mapOption(guard.members, func(members []readMember) routeGuard {
			names := []string{}
			for _, member := range members {
				names = append(names, member.object.Name())
			}
			return routeGuard{plan: serverplan.Guarded{Name: name, Argument: words[1]}, members: names}
		})
	}, func(error) mo.Option[routeGuard] { return malformed() })
}
