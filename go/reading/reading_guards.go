package reading

import (
	"cmp"
	"go/token"
	"slices"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
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

// readGuardName reads word of a guard line as a guard's name: one exported Go identifier, the form
// it takes as a field of what routes are served with.
func readGuardName(index projectIndex, text directiveText, word string) reading[string] {
	if !token.IsIdentifier(word) || !token.IsExported(word) {
		return notRead[string](index.refused(text.position, GuardNameInvalid{Text: text.written, Name: word}))
	}
	return readAs(word, nothingFound())
}

// guardDeclaration is a type declaration marked //nocontract:guard, with each of its guard lines.
type guardDeclaration struct {
	declared declaredType
	lines    []directiveText
}

// readGuards reads every sealed refusal marked //nocontract:guard in the project's packages, in
// declaration order, before any route names one.
func readGuards(index projectIndex) guardTable {
	marked := []guardDeclaration{}
	for _, each := range index.ownTypes {
		if lines := directiveTexts(each.comment, guardKind{}); len(lines) > 0 {
			marked = append(marked, guardDeclaration{declared: each, lines: lines})
		}
	}
	slices.SortFunc(marked, func(left, right guardDeclaration) int {
		return cmp.Or(cmp.Compare(packagePath(left.declared.object), packagePath(right.declared.object)), index.sourceOrder(left.declared.object.Pos(), right.declared.object.Pos()))
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
	return readOnce(marked.lines, func() reading[namedGuard] { return notRead[namedGuard](nothingFound()) }, func(text directiveText) reading[namedGuard] {
		if len(text.words) != 1 {
			return notRead[namedGuard](index.refused(text.position, GuardDirectiveWordCount{Text: text.written}))
		}
		return andThen(readGuardName(index, text, text.words[0]), func(name string) reading[namedGuard] {
			return outcome.FoldOption(keyed.Entry(guards, name), func(first guardEntry) reading[namedGuard] {
				return notRead[namedGuard](index.refused(marked.declared.spec.Pos(), GuardRepeated{Guard: name, First: index.position(first.declared.spec.Pos()).String()}))
			}, func() reading[namedGuard] {
				read := readGuardRefusal(index, marked.declared)
				return readAs(namedGuard{name: name, entry: guardEntry{declared: marked.declared, members: read.value}}, read.found)
			})
		})
	}, func(second directiveText) reading[namedGuard] {
		return notRead[namedGuard](index.refused(second.position, GuardDirectiveRepeated{Text: second.written}))
	})
}

// readGuardRefusal reads a guard's sealed refusal, refusing each member that succeeds: a refusal
// holds only failures, which the generated code of the refusal's package answers.
func readGuardRefusal(index projectIndex, declared declaredType) reading[[]sealedMember] {
	return andThen(readSealed(index, typeKeyFor(declared.object), declared.spec.Pos()), func(sealed sealedRead) reading[[]sealedMember] {
		refused := collected(sealed.successes, func(member succeededMember) reading[succeededMember] {
			object := member.member.declared.object
			return notRead[succeededMember](index.refused(object.Pos(), GuardMemberSucceeds{Member: object.Name(), Status: member.status.Code()}))
		})
		answered := collected(sealed.failures, func(member sealedMember) reading[serverplan.Member] {
			return writableAnswer(index, member, declared.loaded.PkgPath, member.declared.spec.Pos())
		})
		return mapped(paired(refused, answered), func(pair[[]succeededMember, []serverplan.Member]) []sealedMember { return sealed.failures })
	})
}

// routeGuard is the guard a handler's //nocontract:guard line names, with the members of its
// refusal: none when the handler names no guard.
type routeGuard struct {
	plan    serverplan.RouteGuard
	members []description.Member
}

// readRouteGuard reads a handler's //nocontract:guard line, naming a declared guard and the one
// argument it is handed.
func readRouteGuard(index projectIndex, guards guardTable, texts []directiveText) reading[routeGuard] {
	return readOnce(texts, func() reading[routeGuard] {
		return readAs(routeGuard{plan: serverplan.Unguarded{}, members: []description.Member{}}, nothingFound())
	}, func(text directiveText) reading[routeGuard] { return readNamedRouteGuard(index, guards, text) }, func(second directiveText) reading[routeGuard] {
		return notRead[routeGuard](index.refused(second.position, GuardDirectiveRepeated{Text: second.written}))
	})
}

// readNamedRouteGuard reads a handler's guard line as the declared guard it names and the
// argument the guard is handed.
func readNamedRouteGuard(index projectIndex, guards guardTable, line directiveText) reading[routeGuard] {
	if len(line.words) != 2 {
		return notRead[routeGuard](index.refused(line.position, RouteGuardWordCount{Text: line.written}))
	}
	argument := line.words[1]
	return andThen(readGuardName(index, line, line.words[0]), func(name string) reading[routeGuard] {
		return outcome.FoldOption(keyed.Entry(guards.guards, name), func(guard guardEntry) reading[routeGuard] {
			return mo.Fold(guard.members, func(members []sealedMember) reading[routeGuard] {
				// The generated server names the refusal, which types what the guard decides, from a
				// package of its own.
				return mapped(reachableName(index, guard.declared.object, "", line.position), func(refusal serverplan.GoName) routeGuard {
					guarding := serverplan.Guarding{Name: name, Refusal: refusal}
					return routeGuard{plan: serverplan.Guarded{Guard: guarding, Argument: argument}, members: described(members)}
				})
			}, func(error) reading[routeGuard] { return notRead[routeGuard](nothingFound()) })
		}, func() reading[routeGuard] {
			return notRead[routeGuard](index.refused(line.position, GuardUndeclared{Guard: name}))
		})
	})
}
