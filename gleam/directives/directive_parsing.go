package directives

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/sequence"
	"github.com/CheckPickerUpper/outcome"
)

type statement struct {
	Position assembling.Position
	Text     string
}

// UnknownDirective identifies a directive whose spelling NoContract does not recognize.
type UnknownDirective struct {
	Position assembling.Position
	Text     string
}

// Error locates the unknown spelling on its declaration.
func (failure UnknownDirective) Error() string {
	return fmt.Sprintf("%s: unknown Gleam directive %q", failure.Position, failure.Text)
}

type directiveArgumentsWrong struct {
	Statement statement
	Expected  string
}

func (failure directiveArgumentsWrong) Error() string {
	return fmt.Sprintf("%s: %q requires %s", failure.Statement.Position, failure.Statement.Text, failure.Expected)
}

type directiveArgumentRefused struct {
	Statement statement
	Cause     error
}

func (failure directiveArgumentRefused) Error() string {
	return fmt.Sprintf("%s: %q: %v", failure.Statement.Position, failure.Statement.Text, failure.Cause)
}

func (failure directiveArgumentRefused) Unwrap() error { return failure.Cause }

type routePathRefused struct {
	Statement statement
	Problem   description.RoutePathProblem
}

func (failure routePathRefused) Error() string {
	return fmt.Sprintf("%s: %q has an invalid route path: %+v", failure.Statement.Position, failure.Statement.Text, failure.Problem)
}

type directiveRepeated struct {
	First    statement
	Repeated statement
}

func (failure directiveRepeated) Error() string {
	return fmt.Sprintf("%s: %q repeats %q", failure.Repeated.Position, failure.Repeated.Text, failure.First.Text)
}

type directiveMisplaced struct {
	Statement statement
	Owner     string
}

func (failure directiveMisplaced) Error() string {
	return fmt.Sprintf("%s: %q cannot appear on %s", failure.Statement.Position, failure.Statement.Text, failure.Owner)
}

//sumtype:decl
type directive interface{ isDirective() }

//ergo:not-an-error
type routeDirective struct {
	Method description.Method
	Path   description.RoutePath
}

// Guard names a compiler-exported guard function and the scope passed to it.
//
//ergo:not-an-error
type Guard struct {
	Module   string
	Function string
	Scope    string
}

// Header binds a native input field to the header spelling written on its route.
//
//ergo:not-an-error
type Header struct {
	Field    string
	Name     string
	Position assembling.Position
}

//ergo:not-an-error
type statusDirective struct{ Status description.Status }

//ergo:not-an-error
type literalDirective struct{ Spelling string }

func (routeDirective) isDirective()   {}
func (Guard) isDirective()            {}
func (Header) isDirective()           {}
func (statusDirective) isDirective()  {}
func (literalDirective) isDirective() {}

type attachedDirective struct {
	statement statement
	value     directive
}

func parseDocumentation(documentation string, position assembling.Position) outcome.Result[[]attachedDirective] {
	statements := []statement{}
	for _, line := range strings.Split(documentation, "\n") {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "nocontract:") {
			statements = append(statements, statement{Position: position, Text: text})
		}
	}
	first := make(map[string]statement, len(statements))
	return sequence.Traverse(statements, func(line statement) outcome.Result[attachedDirective] {
		words := strings.Fields(strings.TrimPrefix(line.Text, "nocontract:"))
		if len(words) == 0 {
			return outcome.Err[attachedDirective](UnknownDirective(line))
		}
		name := words[0]
		return outcome.Fold(parseDirective(line, name, words[1:]), func(parsed directive) outcome.Result[attachedDirective] {
			switch parsed.(type) {
			case Header:
				return outcome.Ok(attachedDirective{statement: line, value: parsed})
			case routeDirective, Guard, statusDirective, literalDirective, nil:
			}
			if previous, seen := first[name]; seen {
				return outcome.Err[attachedDirective](directiveRepeated{First: previous, Repeated: line})
			}
			first[name] = line
			return outcome.Ok(attachedDirective{statement: line, value: parsed})
		}, outcome.Err[attachedDirective])
	})
}

func parseDirective(line statement, name string, arguments []string) outcome.Result[directive] {
	wrong := func(expected string) outcome.Result[directive] {
		return outcome.Err[directive](directiveArgumentsWrong{Statement: line, Expected: expected})
	}
	switch name {
	case "header":
		if len(arguments) != 2 {
			return wrong("one public input field and one HTTP header name")
		}
		return outcome.Ok[directive](Header{Field: arguments[0], Name: arguments[1], Position: line.Position})
	case "route":
		if len(arguments) != 2 {
			return wrong("an HTTP method and absolute route path")
		}
		return parseRoute(line, arguments[0], arguments[1])
	case "guard":
		if len(arguments) != 2 {
			return wrong("module.function and a scope")
		}
		module, function, found := strings.Cut(arguments[0], ".")
		if !found || module == "" || function == "" || strings.Contains(function, ".") {
			return wrong("a guard name in module.function form")
		}
		return outcome.Ok[directive](Guard{Module: module, Function: function, Scope: arguments[1]})
	case "status":
		if len(arguments) != 1 {
			return wrong("one HTTP status number")
		}
		fail := func(cause error) outcome.Result[directive] {
			return outcome.Err[directive](directiveArgumentRefused{Statement: line, Cause: cause})
		}
		return outcome.Fold(outcome.TupleToResult(strconv.Atoi(arguments[0])), func(number int) outcome.Result[directive] {
			return description.FoldStatus(number, func(status description.Status) outcome.Result[directive] {
				return outcome.Ok[directive](statusDirective{Status: status})
			}, func(cause description.StatusUnanswerable) outcome.Result[directive] { return fail(cause) })
		}, fail)
	case "literal":
		if len(arguments) != 1 {
			return wrong("one wire spelling")
		}
		return outcome.Ok[directive](literalDirective{Spelling: arguments[0]})
	}
	return outcome.Err[directive](UnknownDirective(line))
}

func parseRoute(line statement, verb, path string) outcome.Result[directive] {
	return outcome.FoldOption(description.ParseMethod(verb), func(method description.Method) outcome.Result[directive] {
		switch parsed := description.ParseRoutePath(path).(type) {
		case description.RoutePathRead:
			return outcome.Ok[directive](routeDirective{Method: method, Path: parsed.Path})
		case description.RoutePathRefused:
			return outcome.Err[directive](routePathRefused{Statement: line, Problem: parsed.Problem})
		case nil:
		}
		return outcome.Err[directive](directiveArgumentsWrong{Statement: line, Expected: "an absolute route path"})
	}, func() outcome.Result[directive] {
		return outcome.Err[directive](directiveArgumentsWrong{Statement: line, Expected: "a supported HTTP method"})
	})
}
