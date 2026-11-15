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

// outcomeReading is an outcome read once: its members, or refused.
type outcomeReading struct {
	members mo.Option[[]readMember]
}

// readMember is one member of an outcome with its answer.
type readMember struct {
	object *types.TypeName
	answer description.Answer
}

// memberReading is a member read once, as its answer, or refused.
type memberReading struct {
	answer mo.Option[description.Answer]
}

// memberPlan is how the generated code answers a member.
func memberPlan(member readMember) serverplan.Member {
	return serverplan.Member{Go: goName(member.object), Answer: member.answer}
}

// readOutcome reads a route's sealed outcome, once per outcome, and refuses a member the route's
// package cannot name.
func (reader *projectReader) readOutcome(loaded *packages.Package, outcome types.Type, position token.Pos) mo.Option[outcomeShape] {
	named, isNamed := types.Unalias(outcome).(*types.Named)
	if !isNamed {
		reader.refuse(position, OutcomeNotNamedInterface{Type: types.TypeString(outcome, nil)})
		return mo.None[outcomeShape]()
	}
	key := keyOf(named.Obj())
	read, known := reader.outcomes[key]
	if !known {
		read = outcomeReading{members: reader.readOutcomeMembers(key, position)}
		reader.outcomes[key] = read
	}
	return thenOption(read.members, func(members []readMember) mo.Option[outcomeShape] {
		reached := mapOption(reader.reachableName(named.Obj(), loaded.PkgPath, position), func(name serverplan.GoName) outcomeShape {
			return outcomeShape{name: name, plans: []serverplan.Member{}}
		})
		for _, member := range members {
			memberName := reader.reachableName(member.object, loaded.PkgPath, position)
			reached = thenOption(reached, func(shape outcomeShape) mo.Option[outcomeShape] {
				return mapOption(memberName, func(serverplan.GoName) outcomeShape {
					return outcomeShape{name: shape.name, plans: append(shape.plans, memberPlan(member))}
				})
			})
		}
		return reached
	})
}

// readOutcomeMembers reads a route's outcome, which must answer at least one success.
func (reader *projectReader) readOutcomeMembers(key typeKey, position token.Pos) mo.Option[[]readMember] {
	return thenOption(reader.readSealed(key, position), func(sealed sealedRead) mo.Option[[]readMember] {
		for _, member := range sealed.members {
			if _, isSuccess := member.answer.(description.Succeeds); isSuccess {
				return mo.Some(sealed.members)
			}
		}
		reader.refuse(sealed.declared.spec.Pos(), OutcomeWithoutSuccess{Outcome: key.name})
		return mo.None[[]readMember]()
	})
}

// sealedRead is a sealed interface's declaration and its members, each with its answer.
type sealedRead struct {
	declared declaredType
	members  []readMember
}

// readSealed reads a sealed interface of the project and every member's answer.
func (reader *projectReader) readSealed(key typeKey, position token.Pos) mo.Option[sealedRead] {
	declared, isDeclared := reader.declared[key]
	if !isDeclared {
		reader.refuse(position, DeclarationOutsideProject{Type: key.path + "." + key.name})
		return mo.None[sealedRead]()
	}
	reader.read[key.path] = struct{}{}
	contract, isInterface := declared.object.Type().Underlying().(*types.Interface)
	if !isInterface {
		reader.refuse(position, OutcomeNotNamedInterface{Type: key.name})
		return mo.None[sealedRead]()
	}
	switch declarations.ReadOutcomeDeclaration(declared.comment, contract).(type) {
	case declarations.SealedOutcome:
	case declarations.UnsealedOutcome, declarations.NotAnOutcome, nil:
		reader.refuse(declared.spec.Pos(), OutcomeNotSealed{Outcome: key.name})
		return mo.None[sealedRead]()
	}
	members, complete := []readMember{}, true
	for _, member := range reader.membersOf(declared, contract) {
		mo.Fold(reader.readMember(member), func(answer description.Answer) struct{} {
			members = append(members, readMember{object: member.object, answer: answer})
			return struct{}{}
		}, func(error) struct{} {
			complete = false
			return struct{}{}
		})
	}
	if !complete {
		return mo.None[sealedRead]()
	}
	return mo.Some(sealedRead{declared: declared, members: members})
}

// membersOf is every type of the outcome's package that implements it, in source order. Its
// unexported method seals it, so no other package declares a member.
func (reader *projectReader) membersOf(outcome declaredType, contract *types.Interface) []declaredType {
	scope := outcome.loaded.Types.Scope()
	members := []declaredType{}
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
				reader.refuse(object.Pos(), PointerMember{Member: name})
			}
			continue
		}
		if declared, isDeclared := reader.declared[keyOf(object)]; isDeclared {
			members = append(members, declared)
		}
	}
	slices.SortFunc(members, func(left, right declaredType) int { return cmp.Compare(left.object.Pos(), right.object.Pos()) })
	return members
}

// readMember reads one member's answer and fields, once per member.
func (reader *projectReader) readMember(member declaredType) mo.Option[description.Answer] {
	key := keyOf(member.object)
	if read, known := reader.members[key]; known {
		return read.answer
	}
	answer := reader.readMemberAnswer(member)
	reader.members[key] = memberReading{answer: answer}
	return answer
}

func (reader *projectReader) readMemberAnswer(member declaredType) mo.Option[description.Answer] {
	name := member.object.Name()
	reader.reserve(name, member.spec.Pos())
	named, isNamed := member.object.Type().(*types.Named)
	if isNamed && named.TypeParams().Len() > 0 {
		reader.refuse(member.spec.Pos(), GenericMember{Member: name})
		return mo.None[description.Answer]()
	}
	fields, isStruct := member.object.Type().Underlying().(*types.Struct)
	if !isStruct {
		reader.refuse(member.spec.Pos(), MemberNotStruct{Member: name})
		return mo.None[description.Answer]()
	}
	read, complete := []description.Field{}, true
	for index := range fields.NumFields() {
		field := fields.Field(index)
		if field.Embedded() {
			reader.refuse(field.Pos(), EmbeddedFieldUnsupported{Field: field.Name()})
			complete = false
			continue
		}
		if !field.Exported() {
			continue
		}
		switch reading := reader.fieldOf(field, fields.Tag(index)).(type) {
		case fieldWritten:
			if reading.field.Name == "_tag" || reading.field.Name == "code" {
				reader.refuse(field.Pos(), FieldNameReserved{Field: reading.field.Name})
				complete = false
				continue
			}
			read = append(read, reading.field)
		case fieldRefused:
			complete = false
		case fieldSkipped, nil:
		}
	}
	answer := thenOption(reader.statusOf(member), func(status description.Status) mo.Option[description.Answer] { return reader.answerOf(member, status) })
	return thenOption(answer, func(decided description.Answer) mo.Option[description.Answer] {
		if !complete {
			return mo.None[description.Answer]()
		}
		reader.memberOut = append(reader.memberOut, description.Member{Name: name, Answer: decided, Fields: read})
		return mo.Some(decided)
	})
}

// statusOf is the one status a member's //nocontract:status line declares: a success that
// carries a body, or a failure.
func (reader *projectReader) statusOf(member declaredType) mo.Option[description.Status] {
	name := member.object.Name()
	found := named(directivesIn(member.comment), statusDirective)
	if len(found) == 0 {
		reader.refuse(member.spec.Pos(), StatusDirectiveMissing{Member: name})
		return mo.None[description.Status]()
	}
	malformed := func(text string) mo.Option[description.Status] {
		reader.refuse(found[0].position, StatusDirectiveMalformed{Member: name, Text: text})
		return mo.None[description.Status]()
	}
	if len(found) > 1 {
		return malformed(found[1].argument)
	}
	return mo.Fold(mo.TupleToResult(strconv.Atoi(found[0].argument)), func(number int) mo.Option[description.Status] {
		return mo.Fold(description.ParseStatus(number), mo.Some[description.Status], func(error) mo.Option[description.Status] {
			return malformed(found[0].argument)
		})
	}, func(error) mo.Option[description.Status] { return malformed(found[0].argument) })
}

// answerOf joins a member's status with its ErGo decision, which must agree on whether it fails.
func (reader *projectReader) answerOf(member declaredType, status description.Status) mo.Option[description.Answer] {
	name := member.object.Name()
	marking := declarations.ReadMarking(member.comment, reader.files)
	switch read := status.(type) {
	case description.SuccessStatus:
		switch marking.(type) {
		case declarations.NotAnError:
			return mo.Some[description.Answer](description.Succeeds{Status: read})
		case declarations.Coded:
			reader.refuse(member.spec.Pos(), SuccessWithCode{Member: name, Status: read})
			return mo.None[description.Answer]()
		case declarations.Undecided, declarations.MarkedBoth, declarations.RepeatedCode, nil:
		}
	case description.FailureStatus:
		switch decided := marking.(type) {
		case declarations.Coded:
			return mo.Some[description.Answer](description.Fails{Status: read, Code: ergo.Code(decided.Directive.Text)})
		case declarations.NotAnError:
			reader.refuse(member.spec.Pos(), FailureWithoutCode{Member: name, Status: read})
			return mo.None[description.Answer]()
		case declarations.Undecided, declarations.MarkedBoth, declarations.RepeatedCode, nil:
		}
	case nil:
	}
	reader.refuse(member.spec.Pos(), MemberUndecided{Member: name})
	return mo.None[description.Answer]()
}
