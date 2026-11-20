package gleam

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/CheckPickerUpper/ErGo/codes"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/authored"
	"github.com/CheckPickerUpper/nocontract/gleam/bindings"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/gleam/sequence"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/outcome"
)

// DirectiveRefused preserves every directive involved and the unmet declaration rule.
type DirectiveRefused struct {
	Declaration authored.Declaration
	Requirement string
}

func (failure DirectiveRefused) Error() string {
	return fmt.Sprintf("%s: Gleam metadata requires %s; directives=%+v", failure.Declaration.Position, failure.Requirement, failure.Declaration.Directives)
}

// CodeUnresolved preserves a spelling whose code parser returned no resolved branch.
type CodeUnresolved struct{ Text string }

func (failure CodeUnresolved) Error() string {
	return "no permanent code was parsed for " + failure.Text
}

// CodeRefused preserves the permanent-code parser's complete refusal.
type CodeRefused struct{ Refusal codes.InvalidCode }

func (failure CodeRefused) Error() string {
	return fmt.Sprintf("invalid permanent code: %+v", failure.Refusal)
}

// PrefixRefused preserves the prefix parser's complete refusal.
type PrefixRefused struct{ Refusal codes.InvalidPrefix }

func (failure PrefixRefused) Error() string {
	return fmt.Sprintf("invalid code prefix: %+v", failure.Refusal)
}

func singleDirective(declaration authored.Declaration, name string) outcome.Result[outcome.Option[authored.Directive]] {
	matching := []authored.Directive{}
	for _, directive := range declaration.Directives {
		if directive.Name == name {
			matching = append(matching, directive)
		}
	}
	switch len(matching) {
	case 0:
		return outcome.Ok(outcome.None[authored.Directive]())
	case 1:
		return outcome.Ok(outcome.Some(matching[0]))
	}
	return outcome.Err[outcome.Option[authored.Directive]](DirectiveRefused{Declaration: declaration, Requirement: "one " + name + " directive at most"})
}

func (index projectIndex) declaration(identity compiler.Identity, constructor string) authored.Declaration {
	return outcome.FoldOption(authored.Constructor(index.metadata, identity, constructor), func(declaration authored.Declaration) authored.Declaration { return declaration }, func() authored.Declaration {
		return authored.Declaration{Position: index.position(identity, constructor), Directives: []authored.Directive{}}
	})
}

func (index projectIndex) literal(identity compiler.Identity, constructor string) outcome.Result[string] {
	declaration := index.declaration(identity, constructor)
	return outcome.Fold(singleDirective(declaration, "literal"), func(written outcome.Option[authored.Directive]) outcome.Result[string] {
		return outcome.FoldOption(written, func(directive authored.Directive) outcome.Result[string] {
			if len(directive.Words) != 1 {
				return outcome.Err[string](DirectiveRefused{Declaration: declaration, Requirement: "one wire spelling after literal"})
			}
			return outcome.Ok(directive.Words[0])
		}, func() outcome.Result[string] { return outcome.Ok(constructor) })
	}, outcome.Err[string])
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
	declaration := index.declaration(named.Identity, constructor.Name)
	markers := answerMarkers(constructor.Documentation)
	return outcome.Fold(singleDirective(declaration, "status"), func(status outcome.Option[authored.Directive]) outcome.Result[bindings.Answer] {
		if len(markers) == 0 {
			return outcome.FoldOption(status, func(authored.Directive) outcome.Result[bindings.Answer] {
				return outcome.Err[bindings.Answer](DirectiveRefused{Declaration: declaration, Requirement: "ergleam:not-an-error or ergleam:code on a status constructor"})
			}, func() outcome.Result[bindings.Answer] {
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
			})
		}
		if len(markers) != 1 {
			return outcome.Err[bindings.Answer](DirectiveRefused{Declaration: declaration, Requirement: "exactly one ErGleam success or code marker"})
		}
		return outcome.FoldOption(status, func(status authored.Directive) outcome.Result[bindings.Answer] {
			if len(status.Words) != 1 {
				return outcome.Err[bindings.Answer](DirectiveRefused{Declaration: declaration, Requirement: "one HTTP status number"})
			}
			return outcome.Fold(outcome.TupleToResult(strconv.Atoi(status.Words[0])), func(number int) outcome.Result[bindings.Answer] {
				return description.FoldStatus(number, func(parsed description.Status) outcome.Result[bindings.Answer] {
					return outcome.Fold(answerStatus(markers[0], parsed, declaration), func(answer description.Answer) outcome.Result[bindings.Answer] {
						return outcome.Fold(index.readFields(constructor.Parameters, []compiler.Identity{}), func(fields []bindings.Field) outcome.Result[bindings.Answer] {
							return outcome.Ok[bindings.Answer](bindings.Direct{Constructor: constructor, Answer: answer, Fields: fields})
						}, outcome.Err[bindings.Answer])
					}, outcome.Err[bindings.Answer])
				}, func(cause description.StatusUnanswerable) outcome.Result[bindings.Answer] {
					return outcome.Err[bindings.Answer](cause)
				})
			}, outcome.Err[bindings.Answer])
		}, func() outcome.Result[bindings.Answer] {
			return outcome.Err[bindings.Answer](DirectiveRefused{Declaration: declaration, Requirement: "an HTTP status on every marked answer"})
		})
	}, outcome.Err[bindings.Answer])
}

func answerStatus(marker string, status description.Status, declaration authored.Declaration) outcome.Result[description.Answer] {
	switch parsed := status.(type) {
	case description.SuccessStatus:
		if marker == "ergleam:not-an-error" {
			return outcome.Ok[description.Answer](description.Succeeds{Status: parsed})
		}
	case description.FailureStatus:
		if strings.HasPrefix(marker, "ergleam:code ") {
			return outcome.Fold(parseCode(strings.TrimPrefix(marker, "ergleam:code ")), func(code ergo.Code) outcome.Result[description.Answer] {
				return outcome.Ok[description.Answer](description.Fails{Status: parsed, Code: code})
			}, outcome.Err[description.Answer])
		}
	case nil:
	}
	return outcome.Err[description.Answer](DirectiveRefused{Declaration: declaration, Requirement: "a 2xx status for a non-error or a 4xx/5xx status for an error code"})
}

func parseCode(text string) outcome.Result[ergo.Code] {
	prefix, _, _ := strings.Cut(text, "-")
	switch parsed := codes.ParsePrefix(prefix).(type) {
	case codes.ParsedPrefix:
		switch code := codes.Parse(codes.ParseRequest{Text: text, Prefix: parsed.Prefix}).(type) {
		case codes.ParsedCode:
			return outcome.Ok(code.Code)
		case codes.InvalidCode:
			return outcome.Err[ergo.Code](CodeRefused{Refusal: code})
		case nil:
		}
	case codes.InvalidPrefix:
		return outcome.Err[ergo.Code](PrefixRefused{Refusal: parsed})
	case nil:
	}
	return outcome.Err[ergo.Code](CodeUnresolved{Text: text})
}
