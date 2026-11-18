package description

import (
	"slices"
	"unicode"
	"unicode/utf8"
)

// APIName is the name the TypeScript client declares the API's class under: a TypeScript
// identifier that is no reserved word and no name TypeScript keeps for a class. Only ParseAPIName
// makes one.
type APIName struct{ text string }

// Text is the name as the client writes it.
func (name APIName) Text() string { return name.text }

// APINameReading is what reading a text as an API name ended as.
//
//sumtype:decl
type APINameReading interface{ isAPINameReading() }

// APINameRead is a text that names the API's class.
//
//ergo:not-an-error
type APINameRead struct{ Name APIName }

// APINameNotIdentifier is a text that cannot name the API's class, and why.
//
//ergo:code NC-1081
//ergo:message {Text} cannot name the API's class in TypeScript: {Flaw, select, IdentifierEmpty {it is empty} CharacterCannotStart {its first character cannot start an identifier} CharacterCannotContinue {a later character cannot be part of an identifier} IdentifierReserved {it is a reserved word, or a type's name a class cannot take}}.
type APINameNotIdentifier struct {
	Text string
	Flaw IdentifierFlaw
}

func (APINameRead) isAPINameReading()          {}
func (APINameNotIdentifier) isAPINameReading() {}

// IdentifierFlaw is why a text is not a TypeScript identifier a class can be declared as.
//
//sumtype:decl
type IdentifierFlaw interface{ isIdentifierFlaw() }

// IdentifierEmpty is a text with no characters.
//
//ergo:not-an-error
type IdentifierEmpty struct{}

// CharacterCannotStart is a text whose first character cannot start an identifier.
//
//ergo:not-an-error
type CharacterCannotStart struct{ Character string }

// CharacterCannotContinue is a text holding, after its first character, one no identifier holds,
// and the byte offset it is at.
//
//ergo:not-an-error
type CharacterCannotContinue struct {
	Character string
	Offset    int
}

// IdentifierReserved is a text that is a reserved word, or a type's name a class cannot take.
//
//ergo:not-an-error
type IdentifierReserved struct{}

func (IdentifierEmpty) isIdentifierFlaw()         {}
func (CharacterCannotStart) isIdentifierFlaw()    {}
func (CharacterCannotContinue) isIdentifierFlaw() {}
func (IdentifierReserved) isIdentifierFlaw()      {}

// reservedClassNames is every word ECMAScript reserves, in strict code and in modules, and every
// predefined type name TypeScript refuses as a class's name.
func reservedClassNames() []string {
	return []string{
		"any", "await", "bigint", "boolean", "break", "case", "catch", "class", "const", "continue", "debugger", "default",
		"delete", "do", "else", "enum", "export", "extends", "false", "finally", "for", "function", "if", "implements",
		"import", "in", "instanceof", "interface", "let", "never", "new", "null", "number", "object", "package",
		"private", "protected", "public", "return", "static", "string", "super", "switch", "symbol", "this", "throw",
		"true", "try", "typeof", "undefined", "unknown", "var", "void", "while", "with", "yield",
	}
}

// startsIdentifier is whether character can start a TypeScript identifier: ECMAScript's ID_Start,
// $ or _.
func startsIdentifier(character rune) bool {
	return character == '$' || character == '_' || unicode.In(character, unicode.L, unicode.Nl, unicode.Other_ID_Start)
}

// continuesIdentifier is whether character can follow the first one of a TypeScript identifier:
// ECMAScript's ID_Continue, $, or a zero-width joiner or non-joiner.
func continuesIdentifier(character rune) bool {
	return startsIdentifier(character) || character == '‌' || character == '‍' ||
		unicode.In(character, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc, unicode.Other_ID_Continue)
}

// ParseAPIName reads text as the name of the API's class.
func ParseAPIName(text string) APINameReading {
	first, width := utf8.DecodeRuneInString(text)
	switch {
	case text == "":
		return APINameNotIdentifier{Text: text, Flaw: IdentifierEmpty{}}
	case !startsIdentifier(first):
		return APINameNotIdentifier{Text: text, Flaw: CharacterCannotStart{Character: string(first)}}
	}
	for offset, character := range text[width:] {
		if !continuesIdentifier(character) {
			return APINameNotIdentifier{Text: text, Flaw: CharacterCannotContinue{Character: string(character), Offset: width + offset}}
		}
	}
	if slices.Contains(reservedClassNames(), text) {
		return APINameNotIdentifier{Text: text, Flaw: IdentifierReserved{}}
	}
	return APINameRead{Name: APIName{text: text}}
}
