package description

import (
	"strings"
	"unicode/utf8"

	ergo "github.com/CheckPickerUpper/ErGo"

	"github.com/CheckPickerUpper/nocontract/messages"
)

// HeaderName is an RFC 9110 field name in lowercase, built by ParseHeaderName.
type HeaderName struct{ text string }

// Text is the canonical lowercase name used in requests and client schemas.
func (name HeaderName) Text() string { return name.text }

// HeaderNameReading is a checked header name or the reason its spelling was refused.
//
//sumtype:decl
type HeaderNameReading interface{ isHeaderNameReading() }

// HeaderNameRead is a name the shared header contract accepts.
//
//ergo:not-an-error
type HeaderNameRead struct{ Name HeaderName }

// HeaderNameRefused is a written name the shared header contract cannot accept.
//
//ergo:not-an-error
type HeaderNameRefused struct{ Problem HeaderNameProblem }

func (HeaderNameRead) isHeaderNameReading()    {}
func (HeaderNameRefused) isHeaderNameReading() {}

// HeaderNameProblem is the precise cause of a refused header spelling.
//
//sumtype:decl
type HeaderNameProblem interface {
	isHeaderNameProblem()
	ErrorCode() ergo.Code
	Report(language messages.Language) ergo.Report
	Causes() []error
}

// HeaderNameEmpty is a declaration whose written header name is empty.
//
//ergo:code NC-1153
//ergo:message The header name {Name} is empty.
type HeaderNameEmpty struct{ Name string }

// HeaderNameInvalid is a written name holding a character outside the ASCII token grammar.
//
//ergo:code NC-1154
//ergo:message The header name {Name} holds {Character} at byte {Offset}, which is not an ASCII field-name token character.
type HeaderNameInvalid struct {
	Name      string
	Character string
	Offset    int
}

// HeaderNameReserved is a header whose dedicated HTTP feature owns its declaration.
//
//ergo:code NC-1155
//ergo:message The header {Name} is reserved for its dedicated HTTP feature and cannot be a generic header parameter.
type HeaderNameReserved struct{ Name string }

func (HeaderNameEmpty) isHeaderNameProblem()    {}
func (HeaderNameInvalid) isHeaderNameProblem()  {}
func (HeaderNameReserved) isHeaderNameProblem() {}

// ParseHeaderName accepts exactly RFC 9110's nonempty ASCII token grammar, normalizes its name,
// and refuses generic declarations for fields represented by dedicated OpenAPI features.
func ParseHeaderName(text string) HeaderNameReading {
	if text == "" {
		return HeaderNameRefused{Problem: HeaderNameEmpty{Name: text}}
	}
	for offset, character := range text {
		switch {
		case strings.ContainsRune("!#$%&'*+-.^_`|~", character), character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9':
			continue
		default:
			_, width := utf8.DecodeRuneInString(text[offset:])
			return HeaderNameRefused{Problem: HeaderNameInvalid{Name: text, Character: text[offset : offset+width], Offset: offset}}
		}
	}
	canonical := strings.ToLower(text)
	switch canonical {
	case "accept", "content-type", "authorization", "cookie":
		return HeaderNameRefused{Problem: HeaderNameReserved{Name: text}}
	}
	return HeaderNameRead{Name: HeaderName{text: canonical}}
}
