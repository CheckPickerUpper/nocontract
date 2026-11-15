package reading

import (
	"cmp"
	"go/token"
	"go/types"
	"slices"
	"strconv"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/CheckPickerUpper/ErGo/declarations"
	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// outcomeShape is a route's outcome and its members in declaration order, as the generated code
// answers each.
type outcomeShape struct {
	name  serverplan.GoName
	plans []serverplan.Member
}

// sealedMember is one member of a sealed type, as declared, with its answer.
type sealedMember struct {
	declared declaredType
	answer   description.Answer
}

// memberPlan is how the generated code answers a member.
func memberPlan(member sealedMember) serverplan.Member {
	return serverplan.Member{Go: goName(member.declared), Answer: member.answer}
}

// readOutcome reads a route's sealed outcome, which must answer at least one success, refusing a
// member the route's package cannot name.
func readOutcome(index projectIndex, loaded *packages.Package, outcome types.Type, position token.Pos) reading[outcomeShape] {
	named, isNamed := types.Unalias(outcome).(*types.Named)
	if !isNamed {
		return notRead[outcomeShape](index.refused(position, OutcomeNotNamedInterface{Type: types.TypeString(outcome, nil)}))
	}
	members := andThen(readSealed(index, keyOf(named.Obj()), position), func(sealed sealedRead) reading[[]sealedMember] {
		if len(sealed.successes) == 0 {
			return notRead[[]sealedMember](index.refused(sealed.declared.spec.Pos(), OutcomeWithoutSuccess{Outcome: named.Obj().Name()}))
		}
		return readAs(sealed.members, nothingFound())
	})
	return andThen(members, func(read []sealedMember) reading[outcomeShape] {
		return andThen(reachableName(index, named.Obj(), loaded.PkgPath, position), func(name serverplan.GoName) reading[outcomeShape] {
			plans := collected(read, func(member sealedMember) reading[serverplan.Member] {
				return mapped(reachableName(index, member.declared.object, loaded.PkgPath, position), func(serverplan.GoName) serverplan.Member { return memberPlan(member) })
			})
			return mapped(plans, func(plans []serverplan.Member) outcomeShape { return outcomeShape{name: name, plans: plans} })
		})
	})
}

// sealedRead is a sealed interface's declaration and its members, each with its answer: all of
// them in declaration order, and its successes and failures, each decided once.
type sealedRead struct {
	declared  declaredType
	members   []sealedMember
	successes []succeededMember
	failures  []sealedMember
}

// succeededMember is a member that answers with a success, and its status.
type succeededMember struct {
	object *types.TypeName
	status description.SuccessStatus
}

// sealedOf is a sealed interface's members, sorted by how each answers.
func sealedOf(declared declaredType, members []sealedMember) sealedRead {
	sealed := sealedRead{declared: declared, members: members, successes: []succeededMember{}, failures: []sealedMember{}}
	for _, member := range members {
		switch answer := member.answer.(type) {
		case description.Succeeds:
			sealed.successes = append(sealed.successes, succeededMember{object: member.declared.object, status: answer.Status})
		case description.Fails:
			sealed.failures = append(sealed.failures, member)
		case nil:
		}
	}
	return sealed
}

// readSealed reads a sealed interface of the project and every member's answer.
func readSealed(index projectIndex, key typeKey, position token.Pos) reading[sealedRead] {
	declared, isDeclared := index.declared[key]
	if !isDeclared {
		return notRead[sealedRead](index.refused(position, DeclarationOutsideProject{Type: key.path + "." + key.name}))
	}
	contract, isInterface := declared.object.Type().Underlying().(*types.Interface)
	if !isInterface {
		return notRead[sealedRead](index.refused(position, OutcomeNotNamedInterface{Type: key.name}).with(packageRead(key.path)))
	}
	switch declarations.ReadOutcomeDeclaration(declared.comment, contract).(type) {
	case declarations.SealedOutcome:
	case declarations.UnsealedOutcome, declarations.NotAnOutcome, nil:
		return notRead[sealedRead](index.refused(declared.spec.Pos(), OutcomeNotSealed{Outcome: key.name}).with(packageRead(key.path)))
	}
	candidates := membersOf(index, declared, contract)
	members := collected(candidates.members, func(member declaredType) reading[sealedMember] {
		return mapped(readMember(index, member), func(answer description.Answer) sealedMember {
			return sealedMember{declared: member, answer: answer}
		})
	})
	return alongside(mapped(members, func(read []sealedMember) sealedRead { return sealedOf(declared, read) }),
		candidates.found.with(packageRead(key.path)))
}

// memberCandidates is every type of a sealed interface's package that implements it, and the
// refusal of each type only whose pointer implements it.
type memberCandidates struct {
	members []declaredType
	found   found
}

// membersOf is every type of the outcome's package that implements it, in source order. Its
// unexported method seals it, so no other package declares a member.
func membersOf(index projectIndex, outcome declaredType, contract *types.Interface) memberCandidates {
	scope := outcome.loaded.Types.Scope()
	members, finding := []declaredType{}, nothingFound()
	for _, name := range scope.Names() {
		object, isTypeName := scope.Lookup(name).(*types.TypeName)
		if !isTypeName || object.IsAlias() || object == outcome.object {
			continue
		}
		if _, isInterface := object.Type().Underlying().(*types.Interface); isInterface {
			continue
		}
		if !types.Implements(object.Type(), contract) {
			if types.Implements(types.NewPointer(object.Type()), contract) {
				finding = finding.with(index.refused(object.Pos(), PointerMember{Member: name}))
			}
			continue
		}
		if declared, isDeclared := index.declared[keyOf(object)]; isDeclared {
			members = append(members, declared)
		}
	}
	slices.SortFunc(members, func(left, right declaredType) int { return cmp.Compare(left.object.Pos(), right.object.Pos()) })
	return memberCandidates{members: members, found: finding}
}

// readMember reads one member's answer and fields, finding the member whole when every part of it
// is read.
func readMember(index projectIndex, member declaredType) reading[description.Answer] {
	name := member.object.Name()
	named, isNamed := member.object.Type().(*types.Named)
	if isNamed && named.TypeParams().Len() > 0 {
		return notRead[description.Answer](index.refused(member.spec.Pos(), GenericMember{Member: name}))
	}
	fields, isStruct := member.object.Type().Underlying().(*types.Struct)
	if !isStruct {
		return notRead[description.Answer](index.refused(member.spec.Pos(), MemberNotStruct{Member: name}))
	}
	read := collected(fieldsOf(fields), func(field indexedField) reading[mo.Option[description.Field]] {
		return memberField(index, field)
	})
	answer := andThen(statusOf(index, member), func(status description.Status) reading[description.Answer] { return answerOf(index, member, status) })
	return andThen(read, func(written []mo.Option[description.Field]) reading[description.Answer] {
		return andThen(answer, func(decided description.Answer) reading[description.Answer] {
			whole := memberRead(foundMember{key: keyOf(member.object), position: index.position(member.spec.Pos()), member: description.Member{
				Name: name, Answer: decided, Fields: present(written),
			}})
			return readAs(decided, whole)
		})
	})
}

// present is every field that is written, in order.
func present(fields []mo.Option[description.Field]) []description.Field {
	written := []description.Field{}
	for _, field := range fields {
		field.ForEach(func(value description.Field) { written = append(written, value) })
	}
	return written
}

// memberField reads one field of a member: refused when its JSON name is one every answer starts
// with.
func memberField(index projectIndex, field indexedField) reading[mo.Option[description.Field]] {
	return andThen(fieldOf(index, field, []typeKey{}), func(written mo.Option[description.Field]) reading[mo.Option[description.Field]] {
		return mo.Fold(written, func(value description.Field) reading[mo.Option[description.Field]] {
			if slices.Contains(description.ReservedFields(), value.Name) {
				return notRead[mo.Option[description.Field]](index.refused(field.variable.Pos(), FieldNameReserved{Field: value.Name}))
			}
			return readAs(written, nothingFound())
		}, func(error) reading[mo.Option[description.Field]] { return readAs(written, nothingFound()) })
	})
}

// statusOf is the one status a member's //nocontract:status line declares: a success that
// carries a body, or a failure.
func statusOf(index projectIndex, member declaredType) reading[description.Status] {
	name := member.object.Name()
	malformed := func(line directive) reading[description.Status] {
		return notRead[description.Status](index.refused(line.position, StatusDirectiveMalformed{Member: name, Text: line.argument}))
	}
	switch lines := linesOf(member.comment, statusKind{}).(type) {
	case oneLine:
		return mo.Fold(mo.TupleToResult(strconv.Atoi(lines.line.argument)), func(number int) reading[description.Status] {
			return mo.Fold(description.ParseStatus(number), func(status description.Status) reading[description.Status] {
				return readAs(status, nothingFound())
			}, func(error) reading[description.Status] { return malformed(lines.line) })
		}, func(error) reading[description.Status] { return malformed(lines.line) })
	case repeatedLines:
		return malformed(lines.second)
	case noLine, nil:
	}
	return notRead[description.Status](index.refused(member.spec.Pos(), StatusDirectiveMissing{Member: name}))
}

// answerOf joins a member's status with its ErGo decision, which must agree on whether it fails.
func answerOf(index projectIndex, member declaredType, status description.Status) reading[description.Answer] {
	name := member.object.Name()
	marking := declarations.ReadMarking(member.comment, index.files)
	refused := func(problem Problem) reading[description.Answer] {
		return notRead[description.Answer](index.refused(member.spec.Pos(), problem))
	}
	switch read := status.(type) {
	case description.SuccessStatus:
		switch marking.(type) {
		case declarations.NotAnError:
			return readAs[description.Answer](description.Succeeds{Status: read}, nothingFound())
		case declarations.Coded:
			return refused(SuccessWithCode{Member: name, Status: read})
		case declarations.Undecided, declarations.MarkedBoth, declarations.RepeatedCode, nil:
		}
	case description.FailureStatus:
		switch decided := marking.(type) {
		case declarations.Coded:
			return readAs[description.Answer](description.Fails{Status: read, Code: ergo.Code(decided.Directive.Text)}, nothingFound())
		case declarations.NotAnError:
			return refused(FailureWithoutCode{Member: name, Status: read})
		case declarations.Undecided, declarations.MarkedBoth, declarations.RepeatedCode, nil:
		}
	case nil:
	}
	return refused(MemberUndecided{Member: name})
}
