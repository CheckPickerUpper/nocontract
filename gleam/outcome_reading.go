package gleam

import (
	"fmt"
	"slices"
	"strings"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/bindings"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/gleam/directives"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/nocontract/internal/sequence"
	"github.com/CheckPickerUpper/outcome"
)

// DirectiveRefused preserves every directive involved and the unmet declaration rule.
type DirectiveRefused struct {
	Declaration directives.Declaration
	Requirement string
}

// Error locates the declaration and reports the unmet rule.
func (failure DirectiveRefused) Error() string {
	return fmt.Sprintf("%s: Gleam metadata requires %s; directives=%+v", failure.Declaration.Position, failure.Requirement, failure.Declaration.Directives)
}

// ConstructorCodeRefused attaches a code parser refusal to its Gleam constructor.
type ConstructorCodeRefused struct {
	Constructor string
	Declaration directives.Declaration
	Marker      string
	Cause       error
}

// Error identifies the constructor whose permanent code the parser refused.
func (failure ConstructorCodeRefused) Error() string {
	return fmt.Sprintf("%s: Gleam constructor %s: %v", failure.Declaration.Position, failure.Constructor, failure.Cause)
}

// Unwrap preserves the refusal produced by the permanent-code parser.
func (failure ConstructorCodeRefused) Unwrap() error { return failure.Cause }

func (index projectIndex) constructor(identity compiler.Identity, name string) directives.Constructor {
	return outcome.FoldOption(directives.FindConstructor(index.metadata, identity, name), func(parsed directives.Constructor) directives.Constructor {
		return parsed
	}, func() directives.Constructor {
		return directives.Constructor{
			Declaration: directives.Declaration{Position: index.position(identity, name), Directives: []string{}},
			Status:      outcome.None[description.Status](), Literal: outcome.None[string](),
		}
	})
}

func (index projectIndex) literal(identity compiler.Identity, name string) string {
	return outcome.FoldOption(index.constructor(identity, name).Literal, func(spelling string) string { return spelling }, func() string { return name })
}

func (index projectIndex) readOutcome(written compiler.Type, open []compiler.Identity) outcome.Result[bindings.Outcome] {
	named, isNamed := written.(compiler.Named)
	if !isNamed || slices.Contains(open, named.Identity) {
		return outcome.Err[bindings.Outcome](Unsupported{Type: written, Position: typePosition(index, written), Requirement: "a finite named custom outcome type"})
	}
	return outcome.FoldOption(keyed.Entry(index.outcomes, named.Identity), outcome.Ok[bindings.Outcome], func() outcome.Result[bindings.Outcome] {
		return outcome.Fold(index.definition(named), func(definition compiler.Definition) outcome.Result[bindings.Outcome] {
			if len(definition.Constructors) == 0 {
				return outcome.Err[bindings.Outcome](Unsupported{Type: named, Position: typePosition(index, named), Requirement: "public exhaustive constructors"})
			}
			return outcome.Fold(sequence.Traverse(definition.Constructors, func(constructor compiler.Constructor) outcome.Result[bindings.Answer] {
				return index.readAnswer(named, constructor, append(slices.Clone(open), named.Identity))
			}), func(answers []bindings.Answer) outcome.Result[bindings.Outcome] {
				read := bindings.Outcome{Native: named, Answers: answers}
				index.outcomes[named.Identity] = read
				return outcome.Ok(read)
			}, outcome.Err[bindings.Outcome])
		}, outcome.Err[bindings.Outcome])
	})
}

func answerMarkers(documentation string) []string {
	markers := []string{}
	for _, line := range strings.Split(documentation, "\n") {
		text := strings.TrimSpace(line)
		if text == "ergleam:not-an-error" || strings.HasPrefix(text, "ergleam:code ") {
			markers = append(markers, text)
		}
	}
	return markers
}

func (index projectIndex) readAnswer(named compiler.Named, constructor compiler.Constructor, open []compiler.Identity) outcome.Result[bindings.Answer] {
	metadata := index.constructor(named.Identity, constructor.Name)
	declaration := metadata.Declaration
	markers := answerMarkers(constructor.Documentation)
	switch len(markers) {
	case 0:
		return outcome.FoldOption(metadata.Status, func(description.Status) outcome.Result[bindings.Answer] {
			return outcome.Err[bindings.Answer](DirectiveRefused{Declaration: declaration, Requirement: "ergleam:not-an-error or ergleam:code on a status constructor"})
		}, func() outcome.Result[bindings.Answer] {
			return index.forwardedAnswer(constructor, declaration, open)
		})
	case 1:
		return outcome.FoldOption(metadata.Status, func(status description.Status) outcome.Result[bindings.Answer] {
			return outcome.Fold(answerStatus(markers[0], status, constructor.Name, declaration), func(answer description.Answer) outcome.Result[bindings.Answer] {
				return outcome.Fold(index.readFields(constructor.Parameters, []compiler.Identity{}), func(fields []bindings.Field) outcome.Result[bindings.Answer] {
					return outcome.Ok[bindings.Answer](bindings.Direct{Constructor: constructor, Answer: answer, Fields: fields})
				}, outcome.Err[bindings.Answer])
			}, outcome.Err[bindings.Answer])
		}, func() outcome.Result[bindings.Answer] {
			return outcome.Err[bindings.Answer](DirectiveRefused{Declaration: declaration, Requirement: "an HTTP status on every marked answer"})
		})
	}
	return outcome.Err[bindings.Answer](DirectiveRefused{Declaration: declaration, Requirement: "exactly one ErGleam success or code marker"})
}

func (index projectIndex) forwardedAnswer(constructor compiler.Constructor, declaration directives.Declaration, open []compiler.Identity) outcome.Result[bindings.Answer] {
	if len(constructor.Parameters) != 1 {
		return outcome.Err[bindings.Answer](DirectiveRefused{Declaration: declaration, Requirement: "a marked answer or one positional named answer wrapper"})
	}
	parameter := constructor.Parameters[0]
	return outcome.FoldOption(parameter.Label, func(string) outcome.Result[bindings.Answer] {
		return outcome.Err[bindings.Answer](DirectiveRefused{Declaration: declaration, Requirement: "an unlabelled positional parameter on an unmarked answer wrapper"})
	}, func() outcome.Result[bindings.Answer] {
		return outcome.Fold(index.readOutcome(parameter.Type, open), func(inner bindings.Outcome) outcome.Result[bindings.Answer] {
			return outcome.Ok[bindings.Answer](bindings.Forwarded{Constructor: constructor, Inner: inner})
		}, outcome.Err[bindings.Answer])
	})
}

func answerStatus(marker string, status description.Status, constructor string, declaration directives.Declaration) outcome.Result[description.Answer] {
	switch parsed := status.(type) {
	case description.SuccessStatus:
		if marker == "ergleam:not-an-error" {
			return outcome.Ok[description.Answer](description.Succeeds{Status: parsed})
		}
	case description.FailureStatus:
		if strings.HasPrefix(marker, "ergleam:code ") {
			return outcome.Fold(native.ParseFailureCode(strings.TrimPrefix(marker, "ergleam:code ")), func(code ergo.Code) outcome.Result[description.Answer] {
				return outcome.Ok[description.Answer](description.Fails{Status: parsed, Code: code})
			}, func(cause error) outcome.Result[description.Answer] {
				return outcome.Err[description.Answer](ConstructorCodeRefused{Constructor: constructor, Declaration: declaration, Marker: marker, Cause: cause})
			})
		}
	case nil:
	}
	return outcome.Err[description.Answer](DirectiveRefused{Declaration: declaration, Requirement: "a 2xx status for a non-error or a 4xx/5xx status for an error code"})
}
