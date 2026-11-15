package reading

import (
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/fatih/structtag"
	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
)

// indexedField is one field of a struct with its tag.
type indexedField struct {
	variable *types.Var
	tag      string
}

// fieldsOf is every field of a struct, in order, with its tag.
func fieldsOf(fields *types.Struct) []indexedField {
	indexed := []indexedField{}
	for position := range fields.NumFields() {
		indexed = append(indexed, indexedField{variable: fields.Field(position), tag: fields.Tag(position)})
	}
	return indexed
}

// jsonTag is a field's json tag read as encoding/json reads it.
//
//sumtype:decl
type jsonTag interface{ isJSONTag() }

// jsonNamed is a field written under name, its Go name when the tag names none.
//
//ergo:not-an-error
type jsonNamed struct{ name string }

// jsonOmitted is a field tagged json:"-".
//
//ergo:not-an-error
type jsonOmitted struct{}

// jsonOptioned is a field whose tag carries an option, such as omitempty.
//
//ergo:not-an-error
type jsonOptioned struct{ options string }

func (jsonNamed) isJSONTag()    {}
func (jsonOmitted) isJSONTag()  {}
func (jsonOptioned) isJSONTag() {}

func readJSONTag(field indexedField) jsonTag {
	goName := field.variable.Name()
	return mo.Fold(mo.TupleToResult(structtag.Parse(field.tag)), func(tags *structtag.Tags) jsonTag {
		return mo.Fold(mo.TupleToResult(tags.Get("json")), func(found *structtag.Tag) jsonTag {
			switch {
			case len(found.Options) > 0:
				return jsonOptioned{options: strings.Join(found.Options, ",")}
			case found.Name == "-":
				return jsonOmitted{}
			case found.Name == "":
				return jsonNamed{name: goName}
			}
			return jsonNamed{name: found.Name}
		}, func(error) jsonTag { return jsonNamed{name: goName} })
	}, func(error) jsonTag { return jsonNamed{name: goName} })
}

// fieldOf reads one struct field under encoding/json's naming: nothing for an unexported field or
// one tagged json:"-", which encoding/json never writes. An embedded field and a tag option such as
// omitempty are refused. open is every named struct whose fields are being read.
func fieldOf(index projectIndex, field indexedField, open []typeKey) reading[mo.Option[description.Field]] {
	position := field.variable.Pos()
	if field.variable.Embedded() {
		return notRead[mo.Option[description.Field]](index.refused(position, EmbeddedFieldUnsupported{Field: field.variable.Name()}))
	}
	if !field.variable.Exported() {
		return readAs(mo.None[description.Field](), nothingFound())
	}
	switch tag := readJSONTag(field).(type) {
	case jsonNamed:
		return mapped(schemaOf(index, field.variable.Type(), position, open), func(schema description.Schema) mo.Option[description.Field] {
			return mo.Some(description.Field{Name: tag.name, Schema: schema})
		})
	case jsonOmitted:
		return readAs(mo.None[description.Field](), nothingFound())
	case jsonOptioned:
		return notRead[mo.Option[description.Field]](index.refused(position, FieldOptionUnsupported{Field: field.variable.Name(), Option: tag.options}))
	case nil:
	}
	return readAs(mo.None[description.Field](), nothingFound())
}

// structFields reads every written field of a struct that is not an outcome member.
func structFields(index projectIndex, fields *types.Struct, open []typeKey) reading[[]description.Field] {
	read := collected(fieldsOf(fields), func(field indexedField) reading[mo.Option[description.Field]] { return fieldOf(index, field, open) })
	return mapped(read, present)
}

func unsupported(index projectIndex, position token.Pos, written types.Type, flaw TypeFlaw) reading[description.Schema] {
	return notRead[description.Schema](index.refused(position, TypeUnsupported{Type: types.TypeString(written, nil), Flaw: flaw}))
}

// schemaOf is the JSON shape encoding/json writes a value of written as. open is every named
// struct whose fields are being read, which written may not be again.
func schemaOf(index projectIndex, written types.Type, position token.Pos, open []typeKey) reading[description.Schema] {
	switch typed := types.Unalias(written).(type) {
	case *types.Basic:
		return basicSchema(index, typed, position)
	case *types.Named:
		return namedSchema(index, typed, position, open)
	case *types.Slice:
		if basic, isBasic := typed.Elem().Underlying().(*types.Basic); isBasic && basic.Kind() == types.Byte {
			return unsupported(index, position, written, ByteSliceField{})
		}
		return mapped(schemaOf(index, typed.Elem(), position, open), func(item description.Schema) description.Schema { return description.List{Item: item} })
	case *types.Array:
		return mapped(schemaOf(index, typed.Elem(), position, open), func(item description.Schema) description.Schema { return description.List{Item: item} })
	case *types.Map:
		if key, isBasic := typed.Key().Underlying().(*types.Basic); !isBasic || key.Kind() != types.String {
			return unsupported(index, position, written, MapKeyNotText{})
		}
		return mapped(schemaOf(index, typed.Elem(), position, open), func(value description.Schema) description.Schema { return description.Dictionary{Value: value} })
	case *types.Struct:
		return mapped(structFields(index, typed, open), func(fields []description.Field) description.Schema { return description.InlineStruct{Fields: fields} })
	case *types.Pointer:
		return unsupported(index, position, written, PointerField{})
	case *types.Interface:
		return unsupported(index, position, written, InterfaceField{})
	case *types.Tuple, *types.Signature, *types.Chan, *types.Alias, *types.TypeParam, *types.Union, nil:
	}
	return unsupported(index, position, written, ShapelessField{})
}

func basicSchema(index projectIndex, basic *types.Basic, position token.Pos) reading[description.Schema] {
	switch {
	case basic.Info()&types.IsString != 0:
		return readAs[description.Schema](description.Text{}, nothingFound())
	case basic.Info()&types.IsBoolean != 0:
		return readAs[description.Schema](description.Truth{}, nothingFound())
	case basic.Info()&types.IsInteger != 0:
		return readAs[description.Schema](description.Integer{}, nothingFound())
	case basic.Info()&types.IsFloat != 0:
		return readAs[description.Schema](description.Finite{}, nothingFound())
	}
	return unsupported(index, position, basic, ShapelessField{})
}

func namedSchema(index projectIndex, named *types.Named, position token.Pos, open []typeKey) reading[description.Schema] {
	object := named.Obj()
	switch {
	case packagePath(object) == "time" && object.Name() == "Time":
		return readAs[description.Schema](description.Instant{}, nothingFound())
	case packagePath(object) == "github.com/samber/mo" && object.Name() == "Option":
		return mapped(schemaOf(index, named.TypeArgs().At(0), position, open), func(inner description.Schema) description.Schema { return description.Nullable{Inner: inner} })
	}
	if fields, isStruct := named.Underlying().(*types.Struct); isStruct {
		return namedStruct(index, named, fields, position, open)
	}
	if basic, isBasic := named.Underlying().(*types.Basic); isBasic && basic.Info()&types.IsString != 0 {
		if values := stringConstants(named); len(values) > 0 {
			return readAs[description.Schema](description.Literals{Values: values}, nothingFound())
		}
	}
	return schemaOf(index, named.Underlying(), position, open)
}

// stringConstants is every string constant of the named type declared in its package, sorted.
func stringConstants(named *types.Named) []string {
	values := []string{}
	mo.EmptyableToOption(named.Obj().Pkg()).ForEach(func(declaring *types.Package) {
		for _, name := range declaring.Scope().Names() {
			if declared, isConstant := declaring.Scope().Lookup(name).(*types.Const); isConstant && types.Identical(declared.Type(), named) {
				values = append(values, constant.StringVal(declared.Val()))
			}
		}
	})
	slices.Sort(values)
	return slices.Compact(values)
}

// namedStruct reads a named struct and refers to it by name, finding it whole when every field is
// read. A struct met again while its own fields are read holds itself.
func namedStruct(index projectIndex, named *types.Named, fields *types.Struct, position token.Pos, open []typeKey) reading[description.Schema] {
	object := named.Obj()
	key := keyOf(object)
	switch {
	case named.TypeParams().Len() > 0 || named.TypeArgs().Len() > 0:
		return notRead[description.Schema](index.refused(position, GenericMember{Member: object.Name()}))
	case slices.Contains(open, key):
		return notRead[description.Schema](index.refused(position, RecursiveStruct{Struct: object.Name()}))
	}
	return andThen(structFields(index, fields, append(slices.Clone(open), key)), func(read []description.Field) reading[description.Schema] {
		whole := structRead(foundStruct{key: key, position: index.position(object.Pos()), named: description.NamedStruct{Name: object.Name(), Fields: read}})
		return readAs[description.Schema](description.StructRef{Name: object.Name()}, whole)
	})
}
