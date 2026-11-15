package reading

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/samber/mo"
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
func parseDirectiveKind(spelled string) mo.Option[directiveKind] {
	switch spelled {
	case "route":
		return mo.Some[directiveKind](routeKind{})
	case "status":
		return mo.Some[directiveKind](statusKind{})
	case "guard":
		return mo.Some[directiveKind](guardKind{})
	}
	return mo.None[directiveKind]()
}

// directive is one //nocontract: line: its kind, the text after it and where it is written.
type directive struct {
	kind     directiveKind
	argument string
	position token.Pos
}

// directiveLine is one //nocontract: line, read as a directive NoContract reads or as unknown.
//
//sumtype:decl
type directiveLine interface{ isDirectiveLine() }

// knownLine is a line of a directive NoContract reads.
//
//ergo:not-an-error
type knownLine struct{ read directive }

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
		body, isDirective := strings.CutPrefix(line.Text, directivePrefix)
		if !isDirective {
			continue
		}
		lines = append(lines, readDirectiveLine(body, line.Pos()))
	}
	return lines
}

func readDirectiveLine(body string, position token.Pos) directiveLine {
	spelled, argument, _ := strings.Cut(body, " ")
	return mo.Fold(parseDirectiveKind(spelled), func(kind directiveKind) directiveLine {
		return knownLine{read: directive{kind: kind, argument: strings.TrimSpace(argument), position: position}}
	}, func(error) directiveLine { return unknownLine{text: directivePrefix + body, position: position} })
}

// directiveLines is how many lines of one directive a declaration's comment holds, each declaration
// taking at most one.
//
//sumtype:decl
type directiveLines interface{ isDirectiveLines() }

// noLine is a comment without the directive.
//
//ergo:not-an-error
type noLine struct{}

// oneLine is a comment with the directive written once.
//
//ergo:not-an-error
type oneLine struct{ line directive }

// repeatedLines is a comment writing the directive again, at the second line.
//
//ergo:not-an-error
type repeatedLines struct{ second directive }

func (noLine) isDirectiveLines()        {}
func (oneLine) isDirectiveLines()       {}
func (repeatedLines) isDirectiveLines() {}

// linesOf is how many times comment writes the directive of kind.
func linesOf(comment *ast.CommentGroup, kind directiveKind) directiveLines {
	kept := []directive{}
	for _, line := range directiveLinesIn(comment) {
		switch read := line.(type) {
		case knownLine:
			if read.read.kind == kind {
				kept = append(kept, read.read)
			}
		case unknownLine, nil:
		}
	}
	switch len(kept) {
	case 0:
		return noLine{}
	case 1:
		return oneLine{line: kept[0]}
	}
	return repeatedLines{second: kept[1]}
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
