package writing

import (
	"fmt"
	"go/format"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/messages"
)

// SharedFieldUnsupported is a field of one of nocontract's own failures whose schema the Go runtime
// declares no type for: only text and a set of string constants have one.
//
//ergo:code NC-1063
//ergo:message The field {Field} of {Failure} has no Go runtime type: only text and a set of string constants have one.
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
func RenderRuntime(failures []description.SharedFailure) mo.Result[[]byte] {
	declared := mo.Ok([]string{})
	for _, failure := range failures {
		declared = mo.Fold(declared, func(texts []string) mo.Result[[]string] {
			return mo.Fold(sharedFailureText(failure), func(text string) mo.Result[[]string] {
				return mo.Ok(append(slices.Clone(texts), text))
			}, mo.Err[[]string])
		}, mo.Err[[]string])
	}
	return mo.Fold(declared, func(texts []string) mo.Result[[]byte] {
		names := newImports(serverplan.NoContractPath, serverplan.ServesNoRoutes{})
		members := []serverplan.Member{}
		for _, failure := range failures {
			members = append(members, serverplan.Member{
				Go: serverplan.GoName{Package: serverplan.NoContractPath, Name: failure.Name}, Answer: failure.Member().Answer,
			})
		}
		name := serverplan.AnswerPrefix + serverplan.SharedFailureType
		body := fmt.Sprintf("\n// %s is a failure nocontract answers on a route's behalf, outside its handler.\n//\n//sumtype:decl\ntype %s interface{ is%s() }\n",
			serverplan.SharedFailureType, serverplan.SharedFailureType, serverplan.SharedFailureType) +
			strings.Join(texts, "") +
			fmt.Sprintf("\n// %s answers each failure nocontract answers on a route's behalf\n// with its status, tag and code.", name) +
			answerText(answerFunction{name: name, sealed: serverplan.SharedFailureType, members: members}, names)
		file := description.GeneratedHeader + "\npackage " + serverplan.NoContractPackage + "\n\n" + names.declaration() + "\n" + body
		return mo.TupleToResult(format.Source([]byte(file)))
	}, mo.Err[[]byte])
}

// sharedFailureText is one shared failure's type, carrying the code and message ErGo reads, the type
// of each of its fields that holds one of a set of string constants, with the constants, and its
// place in the sealed type.
func sharedFailureText(failure description.SharedFailure) mo.Result[string] {
	fields, literals := []string{}, &strings.Builder{}
	for _, field := range failure.Fields {
		goField := goFieldName(field.Name)
		switch schema := field.Schema.(type) {
		case description.Text:
			fields = append(fields, fmt.Sprintf("\t%s string `json:%q`\n", goField, field.Name))
		case description.Literals:
			literalType := failure.Name + goField
			fields = append(fields, fmt.Sprintf("\t%s %s `json:%q`\n", goField, literalType, field.Name))
			fmt.Fprintf(literals, "\n// %s is which of its values a %s's %s field holds.\ntype %s string\n\n// The values a %s's %s field holds.\nconst (\n",
				literalType, failure.Name, field.Name, literalType, failure.Name, field.Name)
			for _, value := range schema.Values {
				fmt.Fprintf(literals, "\t%s%s %s = %s\n", literalType, goFieldName(value), literalType, strconv.Quote(value))
			}
			literals.WriteString(")\n")
		case description.Integer, description.Finite, description.Truth, description.Instant, description.List, description.Dictionary,
			description.Nullable, description.StructRef, description.InlineStruct, nil:
			return mo.Err[string](SharedFieldUnsupported{Failure: failure.Name, Field: field.Name})
		}
	}
	return mo.Ok(fmt.Sprintf("\n// %s\n//\n//ergo:code %s\n//ergo:message %s\ntype %s struct {\n%s}\n%s\nfunc (%s) is%s() {}\n",
		failure.Doc, failure.Code, messageText(failure.Message), failure.Name, strings.Join(fields, ""), literals.String(), failure.Name, serverplan.SharedFailureType))
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
