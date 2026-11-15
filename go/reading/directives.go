package reading

import (
	"go/ast"
	"go/token"
	"strings"
)

// The directives NoContract reads, written in Go's directive form: no space after //.
const (
	routeDirective  = "nocontract:route"
	statusDirective = "nocontract:status"
	guardDirective  = "nocontract:guard"
)

// directive is one //nocontract: line: its name, the text after it and where it is written.
type directive struct {
	name     string
	argument string
	position token.Pos
}

// directivesIn is every //nocontract: directive of a comment, in order.
func directivesIn(comment *ast.CommentGroup) []directive {
	found := []directive{}
	for _, line := range comment.List {
		body, isDirective := strings.CutPrefix(line.Text, "//nocontract:")
		if !isDirective {
			continue
		}
		name, argument, _ := strings.Cut(body, " ")
		found = append(found, directive{name: "nocontract:" + name, argument: strings.TrimSpace(argument), position: line.Pos()})
	}
	return found
}

// named is the directives called name.
func named(found []directive, name string) []directive {
	kept := []directive{}
	for _, each := range found {
		if each.name == name {
			kept = append(kept, each)
		}
	}
	return kept
}
