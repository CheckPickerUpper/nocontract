package description

import (
	"strings"
	"unicode"

	ergo "github.com/CheckPickerUpper/ErGo"

	"github.com/CheckPickerUpper/nocontract/messages"
)

// FieldName is the name a field is written under: a key of a JSON object, or the name of the path
// or query parameter it reads. Only ParseFieldName makes one.
type FieldName struct{ text string }

// Text is the name as it is written.
func (name FieldName) Text() string { return name.text }

// TagField is the field every answer names its member in.
func TagField() FieldName { return FieldName{text: "_tag"} }

// CodeField is the field a failure carries its permanent code in.
func CodeField() FieldName { return FieldName{text: "code"} }

// ReservedFields is every field name an answer writes before its member's own fields, which a
// member therefore cannot declare.
func ReservedFields() []FieldName { return []FieldName{TagField(), CodeField()} }

// FieldNameReading is what reading a text as a field's name ended as.
//
//sumtype:decl
type FieldNameReading interface{ isFieldNameReading() }

// FieldNameRead is a text that is a field's name.
//
//ergo:not-an-error
type FieldNameRead struct{ Name FieldName }

// FieldNameRefused is a text that is not a field's name, and why.
//
//ergo:not-an-error
type FieldNameRefused struct{ Problem FieldNameProblem }

func (FieldNameRead) isFieldNameReading()    {}
func (FieldNameRefused) isFieldNameReading() {}

// FieldNameProblem is why a text is not a field's name, as ErGo describes a failure.
//
//sumtype:decl
type FieldNameProblem interface {
	isFieldNameProblem()
	ErrorCode() ergo.Code
	Report(language messages.Language) ergo.Report
	Causes() []error
}

// FieldNameEmpty is a field given an empty name.
//
//ergo:code NC-1108
//ergo:message The field is given an empty name.
type FieldNameEmpty struct{}

// FieldNameUnwritable is a field name holding a character encoding/json does not write in a name,
// which makes it write the field under its Go name instead.
//
//ergo:code NC-1109
//ergo:message The field name {Name} holds {Character}, which encoding/json does not write in a name; a name holds letters, digits, spaces and ASCII punctuation other than quotes, backquotes, backslashes and commas.
type FieldNameUnwritable struct {
	Name      string
	Character string
}

func (FieldNameEmpty) isFieldNameProblem()      {}
func (FieldNameUnwritable) isFieldNameProblem() {}

// nameSymbols is every character other than a letter or a digit that encoding/json writes in a name.
const nameSymbols = "!#$%&()*+-./:;<=>?@[]^_{|}~ "

// ParseFieldName reads text as a field's name: one or more letters, digits and the symbols
// encoding/json writes in a name.
func ParseFieldName(text string) FieldNameReading {
	if text == "" {
		return FieldNameRefused{Problem: FieldNameEmpty{}}
	}
	for _, character := range text {
		if !strings.ContainsRune(nameSymbols, character) && !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			return FieldNameRefused{Problem: FieldNameUnwritable{Name: text, Character: string(character)}}
		}
	}
	return FieldNameRead{Name: FieldName{text: text}}
}
