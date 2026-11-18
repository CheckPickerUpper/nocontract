package reading

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/CheckPickerUpper/outcome"
)

// directivePrefix starts every line NoContract reads, written in Go's directive form: no space
// after //.
const directivePrefix = "//nocontract:"

// directiveKind is one directive NoContract reads.
//
//sumtype:decl
type directiveKind interface{ isDirectiveKind() }

// routeKind is //nocontract:route METHOD /path, on a handler.
//
//ergo:not-an-error
type routeKind struct{}

// statusKind is //nocontract:status CODE, on a member.
//
//ergo:not-an-error
type statusKind struct{}

// guardKind is //nocontract:guard Name, on a guard's refusal, or Name argument, on a handler.
//
//ergo:not-an-error
type guardKind struct{}

func (routeKind) isDirectiveKind()  {}
func (statusKind) isDirectiveKind() {}
func (guardKind) isDirectiveKind()  {}

// parseDirectiveKind is the directive NoContract reads that the text after the prefix names.
func parseDirectiveKind(spelled string) outcome.Option[directiveKind] {
	switch spelled {
	case "route":
		return outcome.Some[directiveKind](routeKind{})
	case "status":
		return outcome.Some[directiveKind](statusKind{})
	case "guard":
		return outcome.Some[directiveKind](guardKind{})
	}
	return outcome.None[directiveKind]()
}

// directiveText is one //nocontract: line of one kind as it is written: the words after the kind,
// the whole line, and where it is. The reader of its kind parses it once into the kind's own value.
type directiveText struct {
	words    []string
	written  string
	position token.Pos
}

// directiveLine is one //nocontract: line, read as a directive NoContract reads or as unknown.
//
//sumtype:decl
type directiveLine interface{ isDirectiveLine() }

// knownLine is a line of a directive NoContract reads, and its kind.
//
//ergo:not-an-error
type knownLine struct {
	kind directiveKind
	text directiveText
}

// unknownLine is a //nocontract: line naming no directive NoContract reads.
//
//ergo:not-an-error
type unknownLine struct {
	text     string
	position token.Pos
}

func (knownLine) isDirectiveLine()   {}
func (unknownLine) isDirectiveLine() {}

// directiveLinesIn is every //nocontract: line of a comment, in order.
func directiveLinesIn(comment *ast.CommentGroup) []directiveLine {
	lines := []directiveLine{}
	for _, line := range comment.List {
		outcome.ForEachOption(outcome.TupleToOption(strings.CutPrefix(line.Text, directivePrefix)), func(body string) {
			lines = append(lines, readDirectiveLine(body, line.Text, line.Pos()))
		})
	}
	return lines
}

// readDirectiveLine reads the text after //nocontract: as the directive it names and the words
// after that name.
func readDirectiveLine(body, written string, position token.Pos) directiveLine {
	spelled, argument, _ := strings.Cut(body, " ")
	return outcome.FoldOption(parseDirectiveKind(spelled), func(kind directiveKind) directiveLine {
		return knownLine{kind: kind, text: directiveText{words: strings.Fields(argument), written: written, position: position}}
	}, func() directiveLine { return unknownLine{text: written, position: position} })
}

// directiveTexts is every line of the directive of kind that comment writes, in order.
func directiveTexts(comment *ast.CommentGroup, kind directiveKind) []directiveText {
	kept := []directiveText{}
	for _, line := range directiveLinesIn(comment) {
		switch read := line.(type) {
		case knownLine:
			if read.kind == kind {
				kept = append(kept, read.text)
			}
		case unknownLine, nil:
		}
	}
	return kept
}

// readOnce reads a declaration's lines of one directive, which it writes at most once: absent when
// it writes none, parse of its line when it writes one, and repeated of the second when it writes
// more.
func readOnce[T any](texts []directiveText, absent func() reading[T], parse func(directiveText) reading[T], repeated func(directiveText) reading[T]) reading[T] {
	switch len(texts) {
	case 0:
		return absent()
	case 1:
		return parse(texts[0])
	}
	return repeated(texts[1])
}

// unknownDirectives is every //nocontract: line of the file naming no directive NoContract reads,
// which would otherwise be ignored without a word.
func unknownDirectives(file *ast.File) []unknownLine {
	unknown := []unknownLine{}
	for _, comment := range file.Comments {
		for _, line := range directiveLinesIn(comment) {
			if read, isUnknown := line.(unknownLine); isUnknown {
				unknown = append(unknown, read)
			}
		}
	}
	return unknown
}
