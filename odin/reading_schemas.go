package odin

import (
	"fmt"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/messages"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

func indexLanguage() messages.Language { return messages.Match("") }

func (index *readIndex) schema(id int, open []int, position assembling.Position) outcome.Result[description.Schema] {
	refuse := func(reason string) outcome.Result[description.Schema] {
		return outcome.Err[description.Schema](DeclarationRefused{Position: position, Declaration: fmt.Sprintf("%#v", index.graph.Types[id]), Rule: reason})
	}
	switch checked := index.graph.Types[id].(type) {
	case compiler.Named:
		if slices.Contains(open, id) {
			return refuse("recursive structs cannot be declared on the wire")
		}
		return outcome.FoldOption(index.declaration(checked.Package, checked.Name), func(declaration compiler.Declaration) outcome.Result[description.Schema] {
			return outcome.FoldOption(index.documentation.typeDirective(declaration, "instant"), func(argument string) outcome.Result[description.Schema] {
				return outcome.Fold(requireType[compiler.String](index.base(id), DeclarationRefused{Position: placed(declaration), Declaration: checked.Name, Rule: "//nocontract:instant requires Instant :: distinct string"}), func(compiler.String) outcome.Result[description.Schema] {
					if checked.Name != "Instant" || argument != "" {
						return refuse("//nocontract:instant designates Instant :: distinct string")
					}
					return outcome.Ok[description.Schema](description.Instant{})
				}, outcome.Err[description.Schema])
			}, func() outcome.Result[description.Schema] {
				return outcome.FoldOption(compiler.Select[compiler.Struct](index.base(id)), func(structure compiler.Struct) outcome.Result[description.Schema] {
					for _, existing := range index.structs {
						if existing.Struct.Name == checked.Name && existing.Position == placed(declaration) {
							return outcome.Ok[description.Schema](description.StructRef{Name: checked.Name})
						}
					}
					return outcome.Fold(index.fields(structure.Fields, append(slices.Clone(open), id), placed(declaration)), func(fields []description.Field) outcome.Result[description.Schema] {
						index.structs = append(index.structs, assembling.DeclaredStruct{Struct: description.NamedStruct{Name: checked.Name, Fields: fields}, Position: placed(declaration)})
						return outcome.Ok[description.Schema](description.StructRef{Name: checked.Name})
					}, outcome.Err[description.Schema])
				}, func() outcome.Result[description.Schema] {
					return index.schema(checked.Base, append(slices.Clone(open), id), placed(declaration))
				})
			})
		}, func() outcome.Result[description.Schema] {
			if checked.Package == "runtime" && strings.HasPrefix(checked.Name, "Maybe(") {
				return index.schema(checked.Base, open, position)
			}
			return outcome.Err[description.Schema](DeclarationRefused{Position: assembling.Position{File: checked.File, Line: checked.Line, Column: checked.Column}, Declaration: checked.Name, Rule: "named type is declared outside the project packages"})
		})
	case compiler.String:
		return outcome.Ok[description.Schema](description.Text{})
	case compiler.Boolean:
		return outcome.Ok[description.Schema](description.Truth{})
	case compiler.Integer:
		return outcome.Fold(integerWidth(checked, position), func(width description.IntegerWidth) outcome.Result[description.Schema] {
			return outcome.Ok[description.Schema](description.Integer{Width: width})
		}, outcome.Err[description.Schema])
	case compiler.Float:
		if checked.Size != 4 && checked.Size != 8 {
			return outcome.Err[description.Schema](FloatWidthUnsupported{Position: position, Bytes: checked.Size})
		}
		return outcome.Ok[description.Schema](description.Finite{})
	case compiler.Enum:
		return outcome.Ok[description.Schema](description.Literals{Values: slices.Clone(compiler.EnumNames(checked))})
	case compiler.Sequence:
		return outcome.Fold(index.schema(checked.Element, open, position), func(item description.Schema) outcome.Result[description.Schema] {
			switch storage := checked.Storage.(type) {
			case compiler.Fixed:
				return outcome.Ok[description.Schema](description.FixedList{Item: item, Length: int64(storage.Length)})
			case compiler.Slice, compiler.Dynamic:
				return outcome.Ok[description.Schema](description.List{Item: item})
			case nil:
				return refuse("sequence storage is absent")
			}
			return refuse("sequence storage has no supported shape")
		}, outcome.Err[description.Schema])
	case compiler.Map:
		return outcome.Fold(index.schema(checked.Element, open, position), func(value description.Schema) outcome.Result[description.Schema] {
			switch key := index.base(checked.Key).(type) {
			case compiler.String:
				return outcome.Ok[description.Schema](description.Dictionary{Key: description.Text{}, Value: value})
			case compiler.Enum:
				return outcome.Ok[description.Schema](description.Dictionary{Key: description.Literals{Values: slices.Clone(compiler.EnumNames(key))}, Value: value})
			case compiler.Named, compiler.Integer, compiler.Float, compiler.Boolean, compiler.Struct, compiler.Union, compiler.Sequence, compiler.Map, compiler.Procedure, compiler.Unsupported, nil:
				return refuse("JSON dictionary keys must be strings or enums")
			}
			return refuse("dictionary key has no supported shape")
		}, outcome.Err[description.Schema])
	case compiler.Union:
		if checked.NoNil || len(checked.Variants) != 1 {
			return refuse("a value union must be Maybe(T); route outcomes are read separately")
		}
		return outcome.Fold(index.schema(checked.Variants[0], open, position), func(inner description.Schema) outcome.Result[description.Schema] {
			return outcome.Ok[description.Schema](description.Nullable{Inner: inner})
		}, outcome.Err[description.Schema])
	case compiler.Struct:
		return outcome.Fold(index.fields(checked.Fields, open, position), func(fields []description.Field) outcome.Result[description.Schema] {
			return outcome.Ok[description.Schema](description.InlineStruct{Fields: fields})
		}, outcome.Err[description.Schema])
	case compiler.Procedure, compiler.Unsupported, nil:
	}
	return refuse("compiler type has no supported JSON shape")
}

func integerWidth(checked compiler.Integer, position assembling.Position) outcome.Result[description.IntegerWidth] {
	if checked.Signed {
		switch checked.Size {
		case 1:
			return outcome.Ok[description.IntegerWidth](description.Signed8{})
		case 2:
			return outcome.Ok[description.IntegerWidth](description.Signed16{})
		case 4:
			return outcome.Ok[description.IntegerWidth](description.Signed32{})
		case 8:
			return outcome.Ok[description.IntegerWidth](description.Signed64{})
		}
	}
	if !checked.Signed {
		switch checked.Size {
		case 1:
			return outcome.Ok[description.IntegerWidth](description.Unsigned8{})
		case 2:
			return outcome.Ok[description.IntegerWidth](description.Unsigned16{})
		case 4:
			return outcome.Ok[description.IntegerWidth](description.Unsigned32{})
		case 8:
			return outcome.Ok[description.IntegerWidth](description.Unsigned64{})
		}
	}
	return outcome.Err[description.IntegerWidth](IntegerWidthUnsupported{Position: position, Bytes: checked.Size, Signed: checked.Signed})
}

func (index *readIndex) fields(fields []compiler.Field, open []int, position assembling.Position) outcome.Result[[]description.Field] {
	read := outcome.Ok([]description.Field{})
	for _, field := range fields {
		read = outcome.Fold(read, func(previous []description.Field) outcome.Result[[]description.Field] {
			return outcome.Fold(wireName(field, position), func(name string) outcome.Result[[]description.Field] {
				return outcome.Fold(index.schema(field.Type, open, position), func(schema description.Schema) outcome.Result[[]description.Field] {
					return outcome.Fold(namedField(name, schema, position), func(field description.Field) outcome.Result[[]description.Field] {
						return outcome.Ok(append(previous, field))
					}, outcome.Err[[]description.Field])
				}, outcome.Err[[]description.Field])
			}, outcome.Err[[]description.Field])
		}, outcome.Err[[]description.Field])
	}
	return read
}

func (index *readIndex) readGuard(source routeSource) outcome.Result[outcome.Option[guardBinding]] {
	return outcome.FoldOption(source.guard, func(guard guardDirective) outcome.Result[outcome.Option[guardBinding]] {
		refusal := DeclarationRefused{Position: placed(source.declaration), Declaration: guard.written, Rule: "guard must take (string, map[string]string) and return a named union with one empty Admitted member"}
		for _, root := range index.graph.Roots {
			if (declarationName{pkg: root.Package, name: root.Name}) != guard.target {
				continue
			}
			return outcome.Fold(requireType[compiler.Procedure](index.graph.Types[root.Type], refusal), func(procedure compiler.Procedure) outcome.Result[outcome.Option[guardBinding]] {
				if len(procedure.Parameters) != 2 || len(procedure.Results) != 1 {
					return outcome.Err[outcome.Option[guardBinding]](refusal)
				}
				return outcome.Fold(requireType[compiler.String](index.graph.Types[procedure.Parameters[0]], refusal), func(compiler.String) outcome.Result[outcome.Option[guardBinding]] {
					return outcome.Fold(requireType[compiler.Map](index.graph.Types[procedure.Parameters[1]], refusal), func(headers compiler.Map) outcome.Result[outcome.Option[guardBinding]] {
						return outcome.Fold(requireType[compiler.String](index.graph.Types[headers.Key], refusal), func(compiler.String) outcome.Result[outcome.Option[guardBinding]] {
							return outcome.Fold(requireType[compiler.String](index.graph.Types[headers.Element], refusal), func(compiler.String) outcome.Result[outcome.Option[guardBinding]] {
								return outcome.Fold(requireType[compiler.Named](index.graph.Types[procedure.Results[0]], refusal), func(compiler.Named) outcome.Result[outcome.Option[guardBinding]] {
									return index.guardMembers(root, procedure.Results[0], guard.scope, refusal)
								}, outcome.Err[outcome.Option[guardBinding]])
							}, outcome.Err[outcome.Option[guardBinding]])
						}, outcome.Err[outcome.Option[guardBinding]])
					}, outcome.Err[outcome.Option[guardBinding]])
				}, outcome.Err[outcome.Option[guardBinding]])
			}, outcome.Err[outcome.Option[guardBinding]])
		}
		return outcome.Err[outcome.Option[guardBinding]](refusal)
	}, func() outcome.Result[outcome.Option[guardBinding]] { return outcome.Ok(outcome.None[guardBinding]()) })
}

func (index *readIndex) guardMembers(root compiler.Root, output int, scope string, refusal DeclarationRefused) outcome.Result[outcome.Option[guardBinding]] {
	return outcome.Fold(requireType[compiler.Union](index.base(output), refusal), func(union compiler.Union) outcome.Result[outcome.Option[guardBinding]] {
		refusals := outcome.Ok([]description.Member{})
		admissions := 0
		for _, variant := range union.Variants {
			refusals = outcome.Fold(refusals, func(previous []description.Member) outcome.Result[[]description.Member] {
				return outcome.FoldOption(findAdmitted(index.graph, variant), func(compiler.Named) outcome.Result[[]description.Member] { admissions++; return outcome.Ok(previous) }, func() outcome.Result[[]description.Member] {
					return outcome.Fold(index.member(variant), func(member description.Member) outcome.Result[[]description.Member] {
						switch member.Answer.(type) {
						case description.Fails:
							return outcome.Ok(append(previous, member))
						case description.Succeeds, nil:
							return outcome.Err[[]description.Member](refusal)
						}
						return outcome.Err[[]description.Member](refusal)
					}, outcome.Err[[]description.Member])
				})
			}, outcome.Err[[]description.Member])
		}
		if admissions != 1 {
			return outcome.Err[outcome.Option[guardBinding]](refusal)
		}
		return outcome.Fold(refusals, func(refusals []description.Member) outcome.Result[outcome.Option[guardBinding]] {
			return outcome.FoldOption(index.declaration(root.Package, root.Name), func(declaration compiler.Declaration) outcome.Result[outcome.Option[guardBinding]] {
				return outcome.Ok(outcome.Some(guardBinding{declaration: declaration, output: union, scope: scope, refusals: refusals}))
			}, func() outcome.Result[outcome.Option[guardBinding]] {
				return outcome.Err[outcome.Option[guardBinding]](refusal)
			})
		}, outcome.Err[outcome.Option[guardBinding]])
	}, outcome.Err[outcome.Option[guardBinding]])
}

func findAdmitted(graph compiler.Graph, id int) outcome.Option[compiler.Named] {
	return outcome.FoldOption(compiler.Select[compiler.Named](graph.Types[id]), func(named compiler.Named) outcome.Option[compiler.Named] {
		if named.Name != "Admitted" {
			return outcome.None[compiler.Named]()
		}
		return outcome.FoldOption(compiler.Select[compiler.Struct](compiler.Base(graph, id)), func(structure compiler.Struct) outcome.Option[compiler.Named] {
			if len(structure.Fields) == 0 {
				return outcome.Some(named)
			}
			return outcome.None[compiler.Named]()
		}, outcome.None[compiler.Named])
	}, outcome.None[compiler.Named])
}
