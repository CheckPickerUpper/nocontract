package writing

import (
	"fmt"
	"go/format"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/messages"
)

// SharedFieldUnsupported is a field of one of nocontract's own failures whose schema the Go runtime
// declares no type for: only text, whole numbers, a set of string constants and a list of text have one.
//
//ergo:code NC-1063
//ergo:message The field {Field} of {Failure} has no Go runtime type: only text, whole numbers, a set of string constants and a list of text have one.
type SharedFieldUnsupported struct {
	Failure string
	Field   string
}

// Error is the failure's message in the source language.
func (refused SharedFieldUnsupported) Error() string {
	return refused.Report(messages.Match("")).Message
}

// RenderRuntime is the Go runtime's own generated file: each of nocontract's own failures as a type
// carrying the code and message ErGo reads, the sealed type holding them, and the function that
// answers each with its status, tag and code.
func RenderRuntime(failures []description.SharedFailure) outcome.Result[[]byte] {
	declared := outcome.Ok([]sharedFailureDeclaration{})
	for _, failure := range failures {
		declared = outcome.Fold(declared, func(declarations []sharedFailureDeclaration) outcome.Result[[]sharedFailureDeclaration] {
			return outcome.Fold(declareSharedFailure(failure), func(declaration sharedFailureDeclaration) outcome.Result[[]sharedFailureDeclaration] {
				return outcome.Ok(append(declarations, declaration))
			}, outcome.Err[[]sharedFailureDeclaration])
		}, outcome.Err[[]sharedFailureDeclaration])
	}
	return outcome.Fold(declared, func(declarations []sharedFailureDeclaration) outcome.Result[[]byte] {
		names := newImports(serverplan.NoContractPath, routeFileImports(serverplan.ServesNoRoutes{}), []string{})
		texts := []string{}
		members := []serverplan.Member{}
		for _, declaration := range declarations {
			texts = append(texts, declaration.text)
			members = append(members, declaration.member)
		}
		name := serverplan.AnswerPrefix + serverplan.SharedFailureType
		body := taggedTypesText() +
			fmt.Sprintf("\n// %s is a failure nocontract answers itself, outside any handler.\n//\n//sumtype:decl\ntype %s interface{ is%s() }\n",
				serverplan.SharedFailureType, serverplan.SharedFailureType, serverplan.SharedFailureType) +
			strings.Join(texts, "") +
			fmt.Sprintf("\n// %s answers each failure nocontract answers itself\n// with its status, tag and code.", name) +
			answerText(answerFunction{name: name, sealed: serverplan.SharedFailureType, members: members}, names)
		file := description.GeneratedHeader + "\npackage " + serverplan.NoContractPackage + "\n\n" + names.declaration() + "\n" + body
		return outcome.TupleToResult(format.Source([]byte(file)))
	}, outcome.Err[[]byte])
}

// taggedTypesText is the JSON object each member is answered as: its tag, a failure's code, then the
// member's own fields, which encoding/json writes into the same object.
func taggedTypesText() string {
	tag := fmt.Sprintf("json:%q", description.TagField().Text())
	code := fmt.Sprintf("json:%q", description.CodeField().Text())
	return fmt.Sprintf("\n// taggedSuccess is a success member's JSON object: its tag, then the member's own fields.\n"+
		"type taggedSuccess[M any] struct {\n\tTag string `%s`\n\tFields M `json:\",embed\"`\n}\n"+
		"\n// taggedFailure is a failure member's JSON object: its tag, its code, then the member's own fields.\n"+
		"type taggedFailure[M any] struct {\n\tTag string `%s`\n\tCode string `%s`\n\tFields M `json:\",embed\"`\n}\n", tag, tag, code)
}

type sharedFailureDeclaration struct {
	text   string
	member serverplan.Member
}

// declareSharedFailure declares the native fields and answer marshalers together, so a whole
// number's Go type and its decimal-text wire representation come from the same field schema.
func declareSharedFailure(failure description.SharedFailure) outcome.Result[sharedFailureDeclaration] {
	fields, literals := []string{}, &strings.Builder{}
	written := []serverplan.WrittenType{}
	for _, field := range failure.Fields {
		name := field.Name.Text()
		goField := goFieldName(name)
		switch schema := field.Schema.(type) {
		case description.Text:
			fields = append(fields, fmt.Sprintf("\t%s string `json:%q`\n", goField, name))
		case description.Integer:
			goType := integerType(schema.Width)
			if goType.Kind() == types.Invalid {
				return outcome.Err[sharedFailureDeclaration](SharedFieldUnsupported{Failure: failure.Name, Field: name})
			}
			fields = append(fields, fmt.Sprintf("\t%s %s `json:%q`\n", goField, goType.Name(), name))
			for _, each := range serverplan.WrittenTypes(serverplan.BodyValue{GoType: goType, Shape: serverplan.WholeShape{Width: schema.Width}}) {
				if !slices.Contains(written, each) {
					written = append(written, each)
				}
			}
		case description.Literals:
			literalType := failure.Name + goField
			fields = append(fields, fmt.Sprintf("\t%s %s `json:%q`\n", goField, literalType, name))
			fmt.Fprintf(literals, "\n// %s is which of its values a %s's %s field holds.\ntype %s string\n\n// The values a %s's %s field holds.\nconst (\n",
				literalType, failure.Name, name, literalType, failure.Name, name)
			for _, value := range schema.Values {
				fmt.Fprintf(literals, "\t%s%s %s = %s\n", literalType, goFieldName(value), literalType, strconv.Quote(value))
			}
			literals.WriteString(")\n")
		case description.List:
			switch schema.Item.(type) {
			case description.Text:
				fields = append(fields, fmt.Sprintf("\t%s []string `json:%q`\n", goField, name))
			case description.Integer, description.Finite, description.Truth, description.Instant, description.Literals, description.List,
				description.FixedList, description.Dictionary, description.Nullable, description.StructRef, description.InlineStruct, nil:
				return outcome.Err[sharedFailureDeclaration](SharedFieldUnsupported{Failure: failure.Name, Field: name})
			}
		case description.Finite, description.Truth, description.Instant, description.FixedList, description.Dictionary,
			description.Nullable, description.StructRef, description.InlineStruct, nil:
			return outcome.Err[sharedFailureDeclaration](SharedFieldUnsupported{Failure: failure.Name, Field: name})
		}
	}
	return outcome.Ok(sharedFailureDeclaration{
		text: fmt.Sprintf("\n// %s\n//\n//ergo:code %s\n//ergo:message %s\ntype %s struct {\n%s}\n%s\nfunc (%s) is%s() {}\n",
			failure.Doc, failure.Code, messageText(failure.Message), failure.Name, strings.Join(fields, ""), literals.String(), failure.Name, serverplan.SharedFailureType),
		member: serverplan.Member{
			Go: serverplan.GoName{Package: serverplan.NoContractPath, Name: failure.Name}, Answer: failure.Member().Answer, Written: written,
		},
	})
}

func integerType(width description.IntegerWidth) *types.Basic {
	switch width.(type) {
	case description.Signed8:
		return types.Typ[types.Int8]
	case description.Signed16:
		return types.Typ[types.Int16]
	case description.Signed32:
		return types.Typ[types.Int32]
	case description.Signed64:
		return types.Typ[types.Int64]
	case description.Unsigned8:
		return types.Typ[types.Uint8]
	case description.Unsigned16:
		return types.Typ[types.Uint16]
	case description.Unsigned32:
		return types.Typ[types.Uint32]
	case description.Unsigned64:
		return types.Typ[types.Uint64]
	case nil:
	}
	return types.Typ[types.Invalid]
}

// messageText is a shared failure's message as ErGo reads it: each field named as the Go runtime
// declares it.
func messageText(parts []description.MessagePart) string {
	text := &strings.Builder{}
	for _, part := range parts {
		switch written := part.(type) {
		case description.MessageText:
			text.WriteString(written.Text)
		case description.MessageField:
			text.WriteString("{" + goFieldName(written.Field) + "}")
		case nil:
		}
	}
	return text.String()
}

// goFieldName is the exported Go name of a JSON name: each word between underscores begun with a
// capital.
func goFieldName(jsonName string) string {
	words := []string{}
	for word := range strings.SplitSeq(jsonName, "_") {
		if word != "" {
			words = append(words, exported(word))
		}
	}
	return strings.Join(words, "")
}
