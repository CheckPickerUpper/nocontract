package gleam

import (
	"fmt"
	"slices"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/bindings"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/gleam/directives"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/nocontract/internal/sequence"
	"github.com/CheckPickerUpper/outcome"
)

// Unsupported preserves the exact compiler type, location and required representation.
type Unsupported struct {
	Type        compiler.Type
	Position    assembling.Position
	Requirement string
}

func (failure Unsupported) Error() string {
	return fmt.Sprintf("%s: Gleam type %+v requires %s", failure.Position, failure.Type, failure.Requirement)
}

// LiteralConstructor is the exact constructor identity and where its spelling is declared.
type LiteralConstructor struct {
	Type     compiler.Identity
	Name     string
	Position assembling.Position
}

// LiteralRepeated preserves the spelling and both constructors that would decode from it.
type LiteralRepeated struct {
	Spelling string
	First    LiteralConstructor
	Repeated LiteralConstructor
}

func (failure LiteralRepeated) Error() string {
	return fmt.Sprintf("Gleam literal %q names both %+v and %+v", failure.Spelling, failure.First, failure.Repeated)
}

type projectIndex struct {
	project  compiler.Package
	metadata map[string]directives.Module
	records  map[compiler.Identity]bindings.Record
	outcomes map[compiler.Identity]bindings.Outcome
}

func (index projectIndex) position(identity compiler.Identity, constructor string) assembling.Position {
	return outcome.FoldOption(directives.FindConstructor(index.metadata, identity, constructor), func(parsed directives.Constructor) assembling.Position { return parsed.Declaration.Position }, func() assembling.Position {
		return assembling.Position{File: identity.Module + ".gleam", Line: 1, Column: 1}
	})
}

func (index projectIndex) unsupported(written compiler.Type, requirement string) outcome.Result[bindings.Value] {
	return outcome.Err[bindings.Value](Unsupported{Type: written, Position: typePosition(index, written), Requirement: requirement})
}

func typePosition(index projectIndex, written compiler.Type) assembling.Position {
	if named, isNamed := written.(compiler.Named); isNamed {
		return index.position(named.Identity, named.Identity.Name)
	}
	return assembling.Position{File: index.project.Name, Line: 1, Column: 1}
}

func (index projectIndex) definition(named compiler.Named) outcome.Result[compiler.Definition] {
	return outcome.FoldOption(keyed.Entry(index.project.Modules, named.Identity.Module), func(module compiler.Module) outcome.Result[compiler.Definition] {
		return outcome.FoldOption(keyed.Entry(module.Types, named.Identity.Name), func(definition compiler.Definition) outcome.Result[compiler.Definition] {
			if definition.Identity != named.Identity || definition.Parameters != 0 || len(named.Arguments) != 0 {
				return outcome.Err[compiler.Definition](Unsupported{Type: named, Position: index.position(named.Identity, named.Identity.Name), Requirement: "an exact public non-generic custom type identity in this package"})
			}
			return outcome.Ok(definition)
		}, func() outcome.Result[compiler.Definition] {
			return outcome.Err[compiler.Definition](Unsupported{Type: named, Position: index.position(named.Identity, named.Identity.Name), Requirement: "a public custom type exported by the compiler"})
		})
	}, func() outcome.Result[compiler.Definition] {
		return outcome.Err[compiler.Definition](Unsupported{Type: named, Position: index.position(named.Identity, named.Identity.Name), Requirement: "a module exported by this package"})
	})
}

func (index projectIndex) readValue(written compiler.Type, open []compiler.Identity) outcome.Result[bindings.Value] {
	switch resolved := written.(type) {
	case compiler.Named:
		return index.readNamed(resolved, open)
	case compiler.Tuple:
		if len(resolved.Elements) == 0 {
			return index.unsupported(written, "a nonempty homogeneous tuple")
		}
		return outcome.Fold(index.readValue(resolved.Elements[0], open), func(item bindings.Value) outcome.Result[bindings.Value] {
			for _, element := range resolved.Elements[1:] {
				if !compiler.Equal(element, resolved.Elements[0]) {
					return index.unsupported(written, "identical resolved tuple element types")
				}
			}
			return outcome.Ok[bindings.Value](bindings.Fixed{Native: resolved, Item: item})
		}, outcome.Err[bindings.Value])
	case compiler.FunctionType, compiler.Variable, nil:
	}
	return index.unsupported(written, "a scalar, record, enum, list, dictionary, Option, or homogeneous tuple")
}

func (index projectIndex) readNamed(named compiler.Named, open []compiler.Identity) outcome.Result[bindings.Value] {
	identity := named.Identity
	switch {
	case identity.Package == "" && identity.Module == "gleam":
		switch identity.Name {
		case "String":
			return outcome.Ok[bindings.Value](bindings.Text{})
		case "Bool":
			return outcome.Ok[bindings.Value](bindings.Truth{})
		case "Int":
			return outcome.Ok[bindings.Value](bindings.Whole{Width: description.Signed64{}})
		case "Float":
			return outcome.Ok[bindings.Value](bindings.Number{})
		case "List":
			if len(named.Arguments) == 1 {
				return outcome.Fold(index.readValue(named.Arguments[0], open), func(item bindings.Value) outcome.Result[bindings.Value] {
					return outcome.Ok[bindings.Value](bindings.List{Native: named, Item: item})
				}, outcome.Err[bindings.Value])
			}
		}
		return index.unsupported(named, "a supported Gleam primitive with its exact arity")
	case identity.Package == "gleam_stdlib" && identity.Module == "gleam/option" && identity.Name == "Option" && len(named.Arguments) == 1:
		return outcome.Fold(index.readValue(named.Arguments[0], open), func(inner bindings.Value) outcome.Result[bindings.Value] {
			return outcome.Ok[bindings.Value](bindings.Nullable{Native: named, Inner: inner})
		}, outcome.Err[bindings.Value])
	case identity.Package == "gleam_stdlib" && identity.Module == "gleam/dict" && identity.Name == "Dict" && len(named.Arguments) == 2:
		return outcome.Fold(index.readValue(named.Arguments[0], open), func(key bindings.Value) outcome.Result[bindings.Value] {
			return outcome.Fold(index.readValue(named.Arguments[1], open), func(item bindings.Value) outcome.Result[bindings.Value] {
				if dictionaryKey, isKey := key.(bindings.DictionaryKey); isKey {
					return outcome.Ok[bindings.Value](bindings.Dictionary{Native: named, Key: dictionaryKey, Item: item})
				}
				return index.unsupported(named, "String or an enum as the dictionary key")
			}, outcome.Err[bindings.Value])
		}, outcome.Err[bindings.Value])
	}
	reference := bindings.RecordRef{Native: named}
	if _, known := index.records[identity]; known || slices.Contains(open, identity) {
		return outcome.Ok[bindings.Value](reference)
	}
	return outcome.Fold(index.definition(named), func(definition compiler.Definition) outcome.Result[bindings.Value] {
		if allNullary(definition.Constructors) {
			literals := make([]bindings.Literal, 0, len(definition.Constructors))
			for _, constructor := range definition.Constructors {
				literals = append(literals, bindings.Literal{Constructor: constructor.Name, Text: index.literal(identity, constructor.Name)})
			}
			repeats := keyed.Repeats(literals, func(literal bindings.Literal) string { return literal.Text })
			if len(repeats) > 0 {
				repeat := repeats[0]
				return outcome.Err[bindings.Value](LiteralRepeated{
					Spelling: repeat.Item.Text,
					First:    LiteralConstructor{Type: identity, Name: repeat.First.Constructor, Position: index.position(identity, repeat.First.Constructor)},
					Repeated: LiteralConstructor{Type: identity, Name: repeat.Item.Constructor, Position: index.position(identity, repeat.Item.Constructor)},
				})
			}
			return outcome.Ok[bindings.Value](bindings.Enumeration{Native: named, Literals: literals})
		}
		if len(definition.Constructors) != 1 {
			return index.unsupported(named, "one labelled record constructor or only nullary enum constructors")
		}
		constructor := definition.Constructors[0]
		return outcome.FoldOption(scalarWrapper(named, constructor), func(wrapper bindings.Wrapped) outcome.Result[bindings.Value] {
			return outcome.Ok[bindings.Value](wrapper)
		}, func() outcome.Result[bindings.Value] {
			if identity.Name == "UInt8" || identity.Name == "UInt16" || identity.Name == "Instant" {
				return index.unsupported(named, "UInt8(Int), UInt16(Int), or Instant(String), with its matching public constructor")
			}
			return outcome.Fold(index.readFields(constructor.Parameters, append(slices.Clone(open), identity)), func(fields []bindings.Field) outcome.Result[bindings.Value] {
				record := bindings.Record{Native: named, Constructor: constructor, Fields: fields}
				index.records[identity] = record
				return outcome.Ok[bindings.Value](reference)
			}, outcome.Err[bindings.Value])
		})
	}, outcome.Err[bindings.Value])
}

func allNullary(constructors []compiler.Constructor) bool {
	if len(constructors) == 0 {
		return false
	}
	for _, constructor := range constructors {
		if len(constructor.Parameters) != 0 {
			return false
		}
	}
	return true
}

func scalarWrapper(named compiler.Named, constructor compiler.Constructor) outcome.Option[bindings.Wrapped] {
	if len(constructor.Parameters) != 1 || constructor.Name != named.Identity.Name {
		return outcome.None[bindings.Wrapped]()
	}
	primitive, namedPrimitive := constructor.Parameters[0].Type.(compiler.Named)
	if !namedPrimitive || primitive.Identity.Package != "" || primitive.Identity.Module != "gleam" || len(primitive.Arguments) != 0 {
		return outcome.None[bindings.Wrapped]()
	}
	switch {
	case named.Identity.Name == "UInt8" && primitive.Identity.Name == "Int":
		return outcome.Some(bindings.Wrapped{Native: named, Constructor: constructor, Inner: bindings.Whole{Width: description.Unsigned8{}}})
	case named.Identity.Name == "UInt16" && primitive.Identity.Name == "Int":
		return outcome.Some(bindings.Wrapped{Native: named, Constructor: constructor, Inner: bindings.Whole{Width: description.Unsigned16{}}})
	case named.Identity.Name == "Instant" && primitive.Identity.Name == "String":
		return outcome.Some(bindings.Wrapped{Native: named, Constructor: constructor, Inner: bindings.Timestamp{}})
	}
	return outcome.None[bindings.Wrapped]()
}

func (index projectIndex) readFields(parameters []compiler.Parameter, open []compiler.Identity) outcome.Result[[]bindings.Field] {
	return sequence.Traverse(parameters, func(parameter compiler.Parameter) outcome.Result[bindings.Field] {
		return outcome.FoldOption(parameter.Label, func(label string) outcome.Result[bindings.Field] {
			switch parsed := description.ParseFieldName(label).(type) {
			case description.FieldNameRead:
				return outcome.Fold(index.readValue(parameter.Type, open), func(value bindings.Value) outcome.Result[bindings.Field] {
					return outcome.Ok(bindings.Field{Name: parsed.Name, Value: value})
				}, outcome.Err[bindings.Field])
			case description.FieldNameRefused:
				return outcome.Err[bindings.Field](Unsupported{Type: parameter.Type, Position: typePosition(index, parameter.Type), Requirement: fmt.Sprintf("a JSON field name: %+v", parsed.Problem)})
			case nil:
			}
			return outcome.Err[bindings.Field](Unsupported{Type: parameter.Type, Position: typePosition(index, parameter.Type), Requirement: "a JSON field name"})
		}, func() outcome.Result[bindings.Field] {
			return outcome.Err[bindings.Field](Unsupported{Type: parameter.Type, Position: typePosition(index, parameter.Type), Requirement: "a label on every record and answer field"})
		})
	})
}
