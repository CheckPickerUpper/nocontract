package reading

import (
	"cmp"
	"go/token"
	"slices"
	"strings"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// guardEntry is a guard's refusal: its declaration and its members, absent when it was refused.
type guardEntry struct {
	declared declaredType
	members  mo.Option[[]sealedMember]
}

// guardTable is every guard the project declares, by name, and what reading them found.
type guardTable struct {
	guards map[string]guardEntry
	found  found
}

// guardName is text read as a guard's name: one exported Go identifier, the form it takes as a
// field of what routes are served with.
func guardName(text string) mo.Option[string] {
	if !token.IsIdentifier(text) || !token.IsExported(text) {
		return mo.None[string]()
	}
	return mo.Some(text)
}

// guardDeclaration is a type declaration marked //nocontract:guard, with its one line.
type guardDeclaration struct {
	declared declaredType
	line     directiveLines
}

// readGuards reads every sealed refusal marked //nocontract:guard in the project's packages, in
// declaration order, before any route names one.
func readGuards(index projectIndex) guardTable {
	marked := []guardDeclaration{}
	for _, each := range index.ownTypes {
		switch line := writtenLines(each.comment, guardKind{}).(type) {
		case oneLine, repeatedLines:
			marked = append(marked, guardDeclaration{declared: each, line: line})
		case noLine, nil:
		}
	}
	slices.SortFunc(marked, func(left, right guardDeclaration) int {
		return cmp.Or(cmp.Compare(packagePath(left.declared.object), packagePath(right.declared.object)), cmp.Compare(left.declared.object.Pos(), right.declared.object.Pos()))
	})
	guards, finding := map[string]guardEntry{}, nothingFound()
	for _, each := range marked {
		step := readGuardDeclaration(index, guards, each)
		finding = finding.with(step.found)
		step.value.ForEach(func(read namedGuard) { guards[read.name] = read.entry })
	}
	return guardTable{guards: guards, found: finding}
}

// namedGuard is one guard's declaration under its name.
type namedGuard struct {
	name  string
	entry guardEntry
}

// readGuardDeclaration reads one declaration marked //nocontract:guard, refusing a malformed
// name or one an earlier declaration took. Its refusal is read even when refused, so routes naming
// the guard are not refused again for it.
func readGuardDeclaration(index projectIndex, guards map[string]guardEntry, marked guardDeclaration) reading[namedGuard] {
	malformed := func(line directive) reading[namedGuard] {
		return notRead[namedGuard](index.refused(line.position, GuardDirectiveMalformed{Text: line.argument}))
	}
	switch lines := marked.line.(type) {
	case oneLine:
		return mo.Fold(guardName(lines.line.argument), func(name string) reading[namedGuard] {
			if first, known := guards[name]; known {
				return notRead[namedGuard](index.refused(marked.declared.spec.Pos(), GuardRepeated{Guard: name, First: index.position(first.declared.spec.Pos()).String()}))
			}
			read := readGuardRefusal(index, marked.declared)
			return readAs(namedGuard{name: name, entry: guardEntry{declared: marked.declared, members: read.value}}, read.found)
		}, func(error) reading[namedGuard] { return malformed(lines.line) })
	case repeatedLines:
		return malformed(lines.second)
	case noLine, nil:
	}
	return notRead[namedGuard](nothingFound())
}

// readGuardRefusal reads a guard's sealed refusal, refusing each member that succeeds: a refusal
// holds only failures.
func readGuardRefusal(index projectIndex, declared declaredType) reading[[]sealedMember] {
	return andThen(readSealed(index, typeKeyFor(declared.object), declared.spec.Pos()), func(sealed sealedRead) reading[[]sealedMember] {
		refused := collected(sealed.successes, func(member succeededMember) reading[succeededMember] {
			return notRead[succeededMember](index.refused(member.object.Pos(), GuardMemberSucceeds{Member: member.object.Name(), Status: member.status.Code()}))
		})
		return andThen(refused, func([]succeededMember) reading[[]sealedMember] { return readAs(sealed.failures, nothingFound()) })
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
func readRouteGuard(index projectIndex, guards guardTable, lines directiveLines) reading[routeGuard] {
	switch read := lines.(type) {
	case oneLine:
		return readNamedRouteGuard(index, guards, read.line)
	case repeatedLines:
		return notRead[routeGuard](index.refused(read.second.position, GuardDirectiveMalformed{Text: read.second.argument}))
	case noLine, nil:
	}
	return readAs(routeGuard{plan: serverplan.Unguarded{}, members: []string{}}, nothingFound())
}

func readNamedRouteGuard(index projectIndex, guards guardTable, line directive) reading[routeGuard] {
	malformed := notRead[routeGuard](index.refused(line.position, GuardDirectiveMalformed{Text: line.argument}))
	name, argument, hasArgument := strings.Cut(line.argument, " ")
	if !hasArgument || strings.Contains(strings.TrimSpace(argument), " ") {
		return malformed
	}
	return mo.Fold(guardName(name), func(name string) reading[routeGuard] {
		guard, declared := guards.guards[name]
		if !declared {
			return notRead[routeGuard](index.refused(line.position, GuardUndeclared{Guard: name}))
		}
		return mo.Fold(guard.members, func(members []sealedMember) reading[routeGuard] {
			names := []string{}
			for _, member := range members {
				names = append(names, member.declared.object.Name())
			}
			return readAs(routeGuard{plan: serverplan.Guarded{Name: name, Argument: strings.TrimSpace(argument)}, members: names}, nothingFound())
		}, func(error) reading[routeGuard] { return notRead[routeGuard](nothingFound()) })
	}, func(error) reading[routeGuard] { return malformed })
}
