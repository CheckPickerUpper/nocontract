package reading

import (
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/CheckPickerUpper/ErGo/declarations"
	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

// outcomeShape is a route's outcome and its members in declaration order, as the generated code
// answers each, and the members it succeeds and fails with, as the API describes them.
type outcomeShape struct {
	name      serverplan.GoName
	plans     []serverplan.Member
	successes description.Successes
	failures  []description.Member
}

// sealedMember is one member of a sealed type, as declared, and as the API describes it.
type sealedMember struct {
	declared declaredType
	member   description.Member
}

// memberPlan is how the generated code answers a member.
func memberPlan(member sealedMember) serverplan.Member {
	return serverplan.Member{Go: goName(member.declared), Answer: member.member.Answer}
}

// described is each member as the API describes it, in order.
func described(members []sealedMember) []description.Member {
	read := []description.Member{}
	for _, member := range members {
		read = append(read, member.member)
	}
	return read
}

// readOutcome reads a route's sealed outcome, refusing one that answers no success and a member the
// route's package cannot name.
func readOutcome(index projectIndex, loaded *packages.Package, outcome types.Type, route routeName) reading[outcomeShape] {
	named, isNamed := types.Unalias(outcome).(*types.Named)
	if !isNamed {
		return notRead[outcomeShape](index.refused(route.position, OutcomeNotNamedInterface{Type: types.TypeString(outcome, nil)}))
	}
	return andThen(readSealed(index, typeKeyFor(named.Obj()), route.position), func(sealed sealedRead) reading[outcomeShape] {
		if len(sealed.successes) == 0 {
			return notRead[outcomeShape](index.refused(route.position, EndpointWithoutSuccess{Endpoint: route.endpoint}))
		}
		successes := description.Successes{First: sealed.successes[0].member.member, Rest: described(succeededMembers(sealed.successes[1:]))}
		return andThen(reachableName(index, named.Obj(), loaded.PkgPath, route.position), func(name serverplan.GoName) reading[outcomeShape] {
			plans := collected(sealed.members, func(member sealedMember) reading[serverplan.Member] {
				return mapped(reachableName(index, member.declared.object, loaded.PkgPath, route.position), func(serverplan.GoName) serverplan.Member { return memberPlan(member) })
			})
			return mapped(plans, func(plans []serverplan.Member) outcomeShape {
				return outcomeShape{name: name, plans: plans, successes: successes, failures: described(sealed.failures)}
			})
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
	member sealedMember
	status description.SuccessStatus
}

// succeededMembers is each success's member, in order.
func succeededMembers(successes []succeededMember) []sealedMember {
	members := []sealedMember{}
	for _, success := range successes {
		members = append(members, success.member)
	}
	return members
}

// sortedByAnswer is a sealed interface's members, sorted by how each answers.
func sortedByAnswer(declared declaredType, members []sealedMember) sealedRead {
	sealed := sealedRead{declared: declared, members: members, successes: []succeededMember{}, failures: []sealedMember{}}
	for _, member := range members {
		switch answer := member.member.Answer.(type) {
		case description.Succeeds:
			sealed.successes = append(sealed.successes, succeededMember{member: member, status: answer.Status})
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
	switch read := declarations.ReadOutcomeDeclaration(declared.comment, contract).(type) {
	case declarations.SealedOutcome:
	case declarations.UnsealedOutcome:
		unsealed := OutcomeNotSealed{Outcome: key.name, Methods: strings.Join(read.Methods, ", ")}
		return notRead[sealedRead](index.refused(declared.spec.Pos(), unsealed).with(packageRead(key.path)))
	case declarations.NotAnOutcome:
		return notRead[sealedRead](index.refused(declared.spec.Pos(), OutcomeNotDeclared{Outcome: key.name}).with(packageRead(key.path)))
	case nil:
		return notRead[sealedRead](packageRead(key.path))
	}
	candidates := sealedMemberCandidates(index, declared, contract)
	members := collected(candidates.members, func(member declaredType) reading[sealedMember] {
		return mapped(readMember(index, member), func(read description.Member) sealedMember {
			return sealedMember{declared: member, member: read}
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
func readMember(index projectIndex, member declaredType) reading[description.Member] {
	name := member.object.Name()
	named, isNamed := member.object.Type().(*types.Named)
	if isNamed && named.TypeParams().Len() > 0 {
		return notRead[description.Member](index.refused(member.spec.Pos(), GenericMember{Member: name}))
	}
	fields, isStruct := member.object.Type().Underlying().(*types.Struct)
	if !isStruct {
		return notRead[description.Member](index.refused(member.spec.Pos(), MemberNotStruct{Member: name}))
	}
	read := structFields(index, fields, []typeKey{})
	answer := andThen(readStatus(index, member), func(status description.Status) reading[description.Answer] { return readAnswer(index, member, status) })
	return andThen(read, func(written []description.Field) reading[description.Member] {
		return andThen(answer, func(decided description.Answer) reading[description.Member] {
			whole := description.Member{Name: name, Answer: decided, Fields: written}
			return readAs(whole, memberRead(foundMember{key: typeKeyFor(member.object), position: index.placed(member.spec.Pos()), member: whole}))
		})
	})
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
	return notRead[description.Answer](markingRefused(index, member, marking))
}

// markingRefused refuses a member's ErGo marking that decides nothing: no decision, both
// decisions, or more than one code.
func markingRefused(index projectIndex, member declaredType, marking declarations.Marking) found {
	name := member.object.Name()
	switch read := marking.(type) {
	case declarations.MarkedBoth:
		return index.refused(member.spec.Pos(), MemberMarkedBoth{Member: name, Codes: ergoCodes(read.Codes)})
	case declarations.RepeatedCode:
		return index.refused(member.spec.Pos(), CodeRepeated{Member: name, Codes: ergoCodes(read.Codes)})
	case declarations.Undecided, declarations.NotAnError, declarations.Coded, nil:
	}
	return index.refused(member.spec.Pos(), MemberUndecided{Member: name})
}

// ergoCodes is the code each //ergo:code directive writes, in order, joined by commas.
func ergoCodes(directives []declarations.Directive) string {
	texts := []string{}
	for _, directive := range directives {
		texts = append(texts, directive.Text)
	}
	return strings.Join(texts, ", ")
}
