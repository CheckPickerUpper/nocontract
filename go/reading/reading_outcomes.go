package reading

import (
	"go/token"
	"go/types"
	"slices"
	"strconv"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/CheckPickerUpper/ErGo/declarations"
	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
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
	members := mapped(readSealed(index, typeKeyFor(named.Obj()), position), func(sealed sealedRead) []sealedMember { return sealed.members })
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

// sortedByAnswer is a sealed interface's members, sorted by how each answers.
func sortedByAnswer(declared declaredType, members []sealedMember) sealedRead {
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
	return outcome.FoldOption(keyed.Entry(index.declared, key), func(declared declaredType) reading[sealedRead] {
		return readDeclaredSealed(index, key, declared, position)
	}, func() reading[sealedRead] {
		return notRead[sealedRead](index.refused(position, DeclarationOutsideProject{Type: key.path + "." + key.name}))
	})
}

// readDeclaredSealed reads a sealed interface the project's syntax declares and every member's
// answer.
func readDeclaredSealed(index projectIndex, key typeKey, declared declaredType, position token.Pos) reading[sealedRead] {
	contract, isInterface := declared.object.Type().Underlying().(*types.Interface)
	if !isInterface {
		return notRead[sealedRead](index.refused(position, OutcomeNotNamedInterface{Type: key.name}).with(packageRead(key.path)))
	}
	switch declarations.ReadOutcomeDeclaration(declared.comment, contract).(type) {
	case declarations.SealedOutcome:
	case declarations.UnsealedOutcome, declarations.NotAnOutcome, nil:
		return notRead[sealedRead](index.refused(declared.spec.Pos(), OutcomeNotSealed{Outcome: key.name}).with(packageRead(key.path)))
	}
	candidates := sealedMemberCandidates(index, declared, contract)
	members := collected(candidates.members, func(member declaredType) reading[sealedMember] {
		return mapped(readMember(index, member), func(answer description.Answer) sealedMember {
			return sealedMember{declared: member, answer: answer}
		})
	})
	return alongside(mapped(members, func(read []sealedMember) sealedRead { return sortedByAnswer(declared, read) }),
		candidates.found.with(packageRead(key.path)))
}

// memberCandidates is every type of a sealed interface's package that implements it, and the
// refusal of each type only whose pointer implements it.
type memberCandidates struct {
	members []declaredType
	found   found
}

// sealedMemberCandidates is every type of the outcome's package that implements it, in source order. Its
// unexported method seals it, so no other package declares a member.
func sealedMemberCandidates(index projectIndex, sealed declaredType, contract *types.Interface) memberCandidates {
	scope := sealed.loaded.Types.Scope()
	members, finding := []declaredType{}, nothingFound()
	for _, name := range scope.Names() {
		object, isTypeName := scope.Lookup(name).(*types.TypeName)
		if !isTypeName || object.IsAlias() || object == sealed.object {
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
		outcome.ForEachOption(keyed.Entry(index.declared, typeKeyFor(object)), func(declared declaredType) { members = append(members, declared) })
	}
	slices.SortFunc(members, func(left, right declaredType) int { return index.sourceOrder(left.object.Pos(), right.object.Pos()) })
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
	read := collected(indexedFields(fields), func(field indexedField) reading[mo.Option[description.Field]] {
		return readField(index, field, []typeKey{})
	})
	answer := andThen(readStatus(index, member), func(status description.Status) reading[description.Answer] { return readAnswer(index, member, status) })
	return andThen(read, func(written []mo.Option[description.Field]) reading[description.Answer] {
		return andThen(answer, func(decided description.Answer) reading[description.Answer] {
			whole := memberRead(foundMember{key: typeKeyFor(member.object), position: index.placed(member.spec.Pos()), member: description.Member{
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

// readStatus is the one status a member's //nocontract:status line declares: a success that
// carries a body, or a failure.
func readStatus(index projectIndex, member declaredType) reading[description.Status] {
	name := member.object.Name()
	return readOnce(directiveTexts(member.comment, statusKind{}), func() reading[description.Status] {
		return notRead[description.Status](index.refused(member.spec.Pos(), StatusDirectiveMissing{Member: name}))
	}, func(text directiveText) reading[description.Status] {
		return readStatusLine(index, name, text)
	}, func(second directiveText) reading[description.Status] {
		return notRead[description.Status](index.refused(second.position, StatusDirectiveRepeated{Member: name, Text: second.written}))
	})
}

// readStatusLine reads a member's //nocontract:status line as one number HTTP answers with,
// refusing a wrong count of words, a word that is not a number and a number HTTP cannot answer
// each on its own.
func readStatusLine(index projectIndex, member string, text directiveText) reading[description.Status] {
	if len(text.words) != 1 {
		return notRead[description.Status](index.refused(text.position, StatusDirectiveWordCount{Member: member, Text: text.written}))
	}
	return outcome.Fold(outcome.TupleToResult(strconv.Atoi(text.words[0])), func(number int) reading[description.Status] {
		return description.FoldStatus(number, func(status description.Status) reading[description.Status] {
			return readAs(status, nothingFound())
		}, func(unanswerable description.StatusUnanswerable) reading[description.Status] {
			return notRead[description.Status](index.refused(text.position, unanswerable))
		})
	}, func(error) reading[description.Status] {
		return notRead[description.Status](index.refused(text.position, StatusNotNumber{Member: member, Text: text.written, Status: text.words[0]}))
	})
}

// readAnswer joins a member's status with its ErGo decision, which must agree on whether it fails.
func readAnswer(index projectIndex, member declaredType, status description.Status) reading[description.Answer] {
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
			return refused(SuccessWithCode{Member: name, Status: read.Code()})
		case declarations.Undecided, declarations.MarkedBoth, declarations.RepeatedCode, nil:
		}
	case description.FailureStatus:
		switch decided := marking.(type) {
		case declarations.Coded:
			return readAs[description.Answer](description.Fails{Status: read, Code: ergo.Code(decided.Directive.Text)}, nothingFound())
		case declarations.NotAnError:
			return refused(FailureWithoutCode{Member: name, Status: read.Code()})
		case declarations.Undecided, declarations.MarkedBoth, declarations.RepeatedCode, nil:
		}
	case nil:
	}
	return refused(MemberUndecided{Member: name})
}
