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

// structState is how far reading one named struct has got.
//
//sumtype:decl
type structState interface{ isStructState() }

// structOpen is a struct whose fields are being read, so meeting it again is recursion.
//
//ergo:not-an-error
type structOpen struct{}

// structDone is a struct read whole.
//
//ergo:not-an-error
type structDone struct{}

// structFailed is a struct with a refused field.
//
//ergo:not-an-error
type structFailed struct{}

func (structOpen) isStructState()   {}
func (structDone) isStructState()   {}
func (structFailed) isStructState() {}

// fieldReading is what reading one exported field ended as.
//
//sumtype:decl
type fieldReading interface{ isFieldReading() }

// fieldWritten is a field encoding/json writes, with its JSON name and schema.
//
//ergo:not-an-error
type fieldWritten struct{ field description.Field }

// fieldSkipped is a field tagged json:"-", which encoding/json never writes.
//
//ergo:not-an-error
type fieldSkipped struct{}

// fieldRefused is a field the API cannot hold; the refusal is recorded.
//
//ergo:not-an-error
type fieldRefused struct{}

func (fieldWritten) isFieldReading() {}
func (fieldSkipped) isFieldReading() {}
func (fieldRefused) isFieldReading() {}

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

func readJSONTag(field *types.Var, tag string) jsonTag {
	return mo.Fold(mo.TupleToResult(structtag.Parse(tag)), func(tags *structtag.Tags) jsonTag {
		return mo.Fold(mo.TupleToResult(tags.Get("json")), func(found *structtag.Tag) jsonTag {
			if len(found.Options) > 0 {
				return jsonOptioned{options: strings.Join(found.Options, ",")}
			}
			if found.Name == "-" {
				return jsonOmitted{}
			}
			if found.Name == "" {
				return jsonNamed{name: field.Name()}
			}
			return jsonNamed{name: found.Name}
		}, func(error) jsonTag { return jsonNamed{name: field.Name()} })
	}, func(error) jsonTag { return jsonNamed{name: field.Name()} })
}

// fieldOf reads an exported field under encoding/json's naming. A tag option such as omitempty
// is refused.
func (reader *projectReader) fieldOf(field *types.Var, tag string) fieldReading {
	switch read := readJSONTag(field, tag).(type) {
	case jsonNamed:
		return mo.Fold(reader.schemaOf(field.Type(), field.Pos()), func(schema description.Schema) fieldReading {
			return fieldWritten{field: description.Field{Name: read.name, Schema: schema}}
		}, func(error) fieldReading { return fieldRefused{} })
	case jsonOmitted:
		return fieldSkipped{}
	case jsonOptioned:
		reader.refuse(field.Pos(), FieldOptionUnsupported{Field: field.Name(), Option: read.options})
		return fieldRefused{}
	case nil:
	}
	return fieldRefused{}
}

// structFields reads the exported fields of a struct that is not an outcome member.
func (reader *projectReader) structFields(fields *types.Struct) mo.Option[[]description.Field] {
	read, complete := []description.Field{}, true
	for index := range fields.NumFields() {
		field := fields.Field(index)
		if field.Embedded() {
			reader.refuse(field.Pos(), EmbeddedFieldUnsupported{Field: field.Name()})
			complete = false
			continue
		}
		if !field.Exported() {
			continue
		}
		switch reading := reader.fieldOf(field, fields.Tag(index)).(type) {
		case fieldWritten:
			read = append(read, reading.field)
		case fieldRefused:
			complete = false
		case fieldSkipped, nil:
		}
	}
	if !complete {
		return mo.None[[]description.Field]()
	}
	return mo.Some(read)
}

func (reader *projectReader) unsupported(position token.Pos, written types.Type, flaw TypeFlaw) mo.Option[description.Schema] {
	reader.refuse(position, TypeUnsupported{Type: types.TypeString(written, nil), Flaw: flaw})
	return mo.None[description.Schema]()
}

// schemaOf is the JSON shape encoding/json writes a value of written as.
func (reader *projectReader) schemaOf(written types.Type, position token.Pos) mo.Option[description.Schema] {
	switch typed := types.Unalias(written).(type) {
	case *types.Basic:
		return reader.basicSchema(typed, position)
	case *types.Named:
		return reader.namedSchema(typed, position)
	case *types.Slice:
		if basic, isBasic := typed.Elem().Underlying().(*types.Basic); isBasic && basic.Kind() == types.Byte {
			return reader.unsupported(position, written, ByteSliceField{})
		}
		return mapOption(reader.schemaOf(typed.Elem(), position), func(item description.Schema) description.Schema { return description.List{Item: item} })
	case *types.Array:
		return mapOption(reader.schemaOf(typed.Elem(), position), func(item description.Schema) description.Schema { return description.List{Item: item} })
	case *types.Map:
		if key, isBasic := typed.Key().Underlying().(*types.Basic); !isBasic || key.Kind() != types.String {
			return reader.unsupported(position, written, MapKeyNotText{})
		}
		return mapOption(reader.schemaOf(typed.Elem(), position), func(value description.Schema) description.Schema { return description.Dictionary{Value: value} })
	case *types.Struct:
		return mapOption(reader.structFields(typed), func(fields []description.Field) description.Schema { return description.InlineStruct{Fields: fields} })
	case *types.Pointer:
		return reader.unsupported(position, written, PointerField{})
	case *types.Interface:
		return reader.unsupported(position, written, InterfaceField{})
	case *types.Tuple, *types.Signature, *types.Chan, *types.Alias, *types.TypeParam, *types.Union, nil:
	}
	return reader.unsupported(position, written, ShapelessField{})
}

func (reader *projectReader) basicSchema(basic *types.Basic, position token.Pos) mo.Option[description.Schema] {
	switch {
	case basic.Info()&types.IsString != 0:
		return mo.Some[description.Schema](description.Text{})
	case basic.Info()&types.IsBoolean != 0:
		return mo.Some[description.Schema](description.Truth{})
	case basic.Info()&types.IsInteger != 0:
		return mo.Some[description.Schema](description.Integer{})
	case basic.Info()&types.IsFloat != 0:
		return mo.Some[description.Schema](description.Finite{})
	}
	return reader.unsupported(position, basic, ShapelessField{})
}

func (reader *projectReader) namedSchema(named *types.Named, position token.Pos) mo.Option[description.Schema] {
	object := named.Obj()
	switch {
	case packagePath(object) == "time" && object.Name() == "Time":
		return mo.Some[description.Schema](description.Instant{})
	case packagePath(object) == "github.com/samber/mo" && object.Name() == "Option":
		return mapOption(reader.schemaOf(named.TypeArgs().At(0), position), func(inner description.Schema) description.Schema { return description.Nullable{Inner: inner} })
	}
	if fields, isStruct := named.Underlying().(*types.Struct); isStruct {
		return reader.namedStruct(named, fields, position)
	}
	if basic, isBasic := named.Underlying().(*types.Basic); isBasic && basic.Info()&types.IsString != 0 {
		if values := stringConstants(named); len(values) > 0 {
			return mo.Some[description.Schema](description.Literals{Values: values})
		}
	}
	return reader.schemaOf(named.Underlying(), position)
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

// namedStruct reads a named struct once and refers to it by name.
func (reader *projectReader) namedStruct(named *types.Named, fields *types.Struct, position token.Pos) mo.Option[description.Schema] {
	object := named.Obj()
	if named.TypeParams().Len() > 0 || named.TypeArgs().Len() > 0 {
		reader.refuse(position, GenericMember{Member: object.Name()})
		return mo.None[description.Schema]()
	}
	key := keyOf(object)
	reference := mo.Some[description.Schema](description.StructRef{Name: object.Name()})
	if state, known := reader.structs[key]; known {
		switch state.(type) {
		case structOpen:
			reader.refuse(position, RecursiveStruct{Struct: object.Name()})
		case structDone:
			return reference
		case structFailed, nil:
		}
		return mo.None[description.Schema]()
	}
	reader.structs[key] = structOpen{}
	reader.reserve(object.Name(), object.Pos())
	return mo.Fold(reader.structFields(fields), func(read []description.Field) mo.Option[description.Schema] {
		reader.structs[key] = structDone{}
		reader.structOut = append(reader.structOut, description.NamedStruct{Name: object.Name(), Fields: read})
		return reference
	}, func(error) mo.Option[description.Schema] {
		reader.structs[key] = structFailed{}
		return mo.None[description.Schema]()
	})
}
