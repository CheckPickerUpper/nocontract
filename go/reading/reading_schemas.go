package reading

import (
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// indexedField is one field of a struct with its tag.
type indexedField struct {
	variable *types.Var
	tag      string
}

// indexedFields is every field of a struct, in order, with its tag.
func indexedFields(fields *types.Struct) []indexedField {
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

// jsonNamed is a field written under the name its tag gives, or its Go name when the tag gives none.
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

// parsedJSONTag is how encoding/json writes a field whose tags are tags.
func parsedJSONTag(field indexedField, tags fieldTags) jsonTag {
	return outcome.FoldOption(tags.lookup("json"), func(found tagValue) jsonTag {
		switch {
		case len(found.options) > 0:
			return jsonOptioned{options: strings.Join(found.options, ",")}
		case found.name == "-":
			return jsonOmitted{}
		case found.name == "":
			return jsonNamed{name: field.variable.Name()}
		}
		return jsonNamed{name: found.name}
	}, func() jsonTag { return jsonNamed{name: field.variable.Name()} })
}

// fieldWritten is a struct field encoding/json writes, the Go field it is written from, and how a
// body reads it.
type fieldWritten struct {
	field    description.Field
	variable *types.Var
	body     serverplan.BodyValue
}

// readField reads one struct field under encoding/json's naming: nothing for an unexported field or
// one tagged json:"-", which encoding/json never writes. An embedded field, a tag reflect cannot
// read, a name encoding/json would not write and a tag option such as omitempty are refused. open is
// every named struct whose fields are being read.
func readField(index projectIndex, field indexedField, open []typeKey) reading[outcome.Option[fieldWritten]] {
	position := field.variable.Pos()
	if field.variable.Embedded() {
		return notRead[outcome.Option[fieldWritten]](index.refused(position, EmbeddedFieldUnsupported{Field: field.variable.Name()}))
	}
	if !field.variable.Exported() {
		return readAs(outcome.None[fieldWritten](), nothingFound())
	}
	return andThen(readFieldTags(index, field), func(tags fieldTags) reading[outcome.Option[fieldWritten]] {
		switch tag := parsedJSONTag(field, tags).(type) {
		case jsonNamed:
			read := paired(readFieldName(index, position, tag.name), readValue(index, field.variable.Type(), field.variable.Type(), position, open))
			return mapped(read, func(read pair[description.FieldName, valueRead]) outcome.Option[fieldWritten] {
				return outcome.Some(fieldWritten{field: description.Field{Name: read.first, Schema: read.second.schema}, variable: field.variable, body: read.second.body})
			})
		case jsonOmitted:
			return readAs(outcome.None[fieldWritten](), nothingFound())
		case jsonOptioned:
			return notRead[outcome.Option[fieldWritten]](index.refused(position, FieldOptionUnsupported{Field: field.variable.Name(), Option: tag.options}))
		case nil:
		}
		return readAs(outcome.None[fieldWritten](), nothingFound())
	})
}

// structFields reads every field encoding/json writes of a struct, each under its own name.
func structFields(index projectIndex, fields *types.Struct, open []typeKey) reading[[]fieldWritten] {
	reads := []reading[outcome.Option[fieldWritten]]{}
	for _, field := range indexedFields(fields) {
		reads = append(reads, readField(index, field, open))
	}
	written := []fieldWritten{}
	for _, each := range valuesRead(reads) {
		outcome.ForEachOption(each, func(field fieldWritten) { written = append(written, field) })
	}
	once := namedOnce(index, written, func(field fieldWritten) writtenName {
		return writtenName{name: field.field.Name, variable: field.variable}
	})
	return mapped(paired(collected(reads, itself), once), func(read pair[[]outcome.Option[fieldWritten], []fieldWritten]) []fieldWritten { return read.second })
}

// describedFields is each field as the description declares it.
func describedFields(written []fieldWritten) []description.Field {
	fields := []description.Field{}
	for _, each := range written {
		fields = append(fields, each.field)
	}
	return fields
}

// bodyFields is each field as a body reads it.
func bodyFields(written []fieldWritten) []serverplan.BodyStructField {
	fields := []serverplan.BodyStructField{}
	for _, each := range written {
		fields = append(fields, serverplan.BodyStructField{GoField: each.variable.Name(), Name: each.field.Name.Text(), Value: each.body})
	}
	return fields
}

func unsupported(index projectIndex, position token.Pos, written types.Type, flaw TypeFlaw) reading[valueRead] {
	return notRead[valueRead](index.refused(position, TypeUnsupported{Type: types.TypeString(written, nil), Flaw: flaw}))
}

// readSchema is the JSON shape encoding/json writes a value of written as. open is every named
// struct whose fields are being read, which written may not be again.
func readSchema(index projectIndex, written types.Type, position token.Pos, open []typeKey) reading[description.Schema] {
	return mapped(readValue(index, written, written, position, open), func(read valueRead) description.Schema { return read.schema })
}

// valueRead is the JSON shape encoding/json writes a value as, and how a body reads it.
type valueRead struct {
	schema description.Schema
	body   serverplan.BodyValue
}

// valueAs is a value written as schema, which a body reads into the Go type as by shape.
func valueAs(schema description.Schema, as types.Type, shape serverplan.BodyShape) valueRead {
	return valueRead{schema: schema, body: serverplan.BodyValue{GoType: types.Unalias(as), Shape: shape}}
}

// readValue is the JSON shape encoding/json writes a value of written as, and how a body reads it
// into as: written itself, or a named type whose underlying type written is. open is every named
// struct whose fields are being read, which written may not be again.
func readValue(index projectIndex, written, as types.Type, position token.Pos, open []typeKey) reading[valueRead] {
	switch typed := types.Unalias(written).(type) {
	case *types.Basic:
		return basicValue(index, typed, as, position)
	case *types.Named:
		return namedValue(index, typed, as, position, open)
	case *types.Slice:
		if basic, isBasic := typed.Elem().Underlying().(*types.Basic); isBasic && basic.Kind() == types.Byte {
			return unsupported(index, position, written, ByteSliceField{})
		}
		return mapped(readValue(index, typed.Elem(), typed.Elem(), position, open), func(item valueRead) valueRead {
			return valueAs(description.List{Item: item.schema}, as, serverplan.ListShape{Item: item.body})
		})
	case *types.Array:
		return mapped(readValue(index, typed.Elem(), typed.Elem(), position, open), func(item valueRead) valueRead {
			fixed := description.FixedList{Item: item.schema, Length: typed.Len()}
			return valueAs(fixed, as, serverplan.ArrayShape{Length: fixed.Length, Item: item.body})
		})
	case *types.Map:
		if key, isBasic := typed.Key().Underlying().(*types.Basic); !isBasic || key.Kind() != types.String {
			return unsupported(index, position, written, MapKeyNotText{})
		}
		key := dictionaryKey(typed.Key())
		return mapped(readValue(index, typed.Elem(), typed.Elem(), position, open), func(value valueRead) valueRead {
			return valueAs(description.Dictionary{Key: key, Value: value.schema}, as,
				serverplan.DictionaryShape{Key: key, KeyType: types.Unalias(typed.Key()), Value: value.body})
		})
	case *types.Struct:
		return mapped(structFields(index, typed, open), func(fields []fieldWritten) valueRead {
			return valueAs(description.InlineStruct{Fields: describedFields(fields)}, as, serverplan.StructShape{Fields: bodyFields(fields)})
		})
	case *types.Pointer:
		return unsupported(index, position, written, PointerField{})
	case *types.Interface:
		return unsupported(index, position, written, InterfaceField{})
	case *types.Tuple, *types.Signature, *types.Chan, *types.Alias, *types.TypeParam, *types.Union, nil:
	}
	return unsupported(index, position, written, ShapelessField{})
}

// basicValue is the JSON shape encoding/json writes a value of a basic type as, and how a body
// reads it into as. An integer's width is its type's own, int, uint and uintptr's the one the
// project's packages were type-checked with.
func basicValue(index projectIndex, basic *types.Basic, as types.Type, position token.Pos) reading[valueRead] {
	whole := func(width description.IntegerWidth) reading[valueRead] {
		return readAs(valueAs(description.Integer{Width: width}, as, serverplan.WholeShape{Width: width}), nothingFound())
	}
	switch basic.Kind() {
	case types.String:
		return readAs(valueAs(description.Text{}, as, serverplan.TextShape{}), nothingFound())
	case types.Bool:
		return readAs(valueAs(description.Truth{}, as, serverplan.TruthShape{}), nothingFound())
	case types.Int8:
		return whole(description.Signed8{})
	case types.Int16:
		return whole(description.Signed16{})
	case types.Int32:
		return whole(description.Signed32{})
	case types.Int64:
		return whole(description.Signed64{})
	case types.Int:
		return whole(signedWidth(index.request.Sizes.Sizeof(basic)))
	case types.Uint8:
		return whole(description.Unsigned8{})
	case types.Uint16:
		return whole(description.Unsigned16{})
	case types.Uint32:
		return whole(description.Unsigned32{})
	case types.Uint64:
		return whole(description.Unsigned64{})
	case types.Uint, types.Uintptr:
		return whole(unsignedWidth(index.request.Sizes.Sizeof(basic)))
	case types.Float32, types.Float64:
		return readAs(valueAs(description.Finite{}, as, serverplan.NumberShape{}), nothingFound())
	case types.Invalid, types.Complex64, types.Complex128, types.UnsafePointer, types.UntypedBool, types.UntypedInt, types.UntypedRune,
		types.UntypedFloat, types.UntypedComplex, types.UntypedString, types.UntypedNil:
	}
	return unsupported(index, position, basic, ShapelessField{})
}

// signedWidth is the width of a signed integer of bytes, which Go sizes at 1, 2, 4 or 8.
func signedWidth(bytes int64) description.IntegerWidth {
	switch bytes {
	case 1:
		return description.Signed8{}
	case 2:
		return description.Signed16{}
	case 4:
		return description.Signed32{}
	}
	return description.Signed64{}
}

// unsignedWidth is the width of an unsigned integer of bytes, which Go sizes at 1, 2, 4 or 8.
func unsignedWidth(bytes int64) description.IntegerWidth {
	switch bytes {
	case 1:
		return description.Unsigned8{}
	case 2:
		return description.Unsigned16{}
	case 4:
		return description.Unsigned32{}
	}
	return description.Unsigned64{}
}

// dictionaryKey is the keys a map whose key type is key takes: the literals of a named string type
// that declares constants, as a value of that type takes, or any text.
func dictionaryKey(key types.Type) description.DictionaryKey {
	if named, isNamed := types.Unalias(key).(*types.Named); isNamed {
		if values := stringConstants(named); len(values) > 0 {
			return description.Literals{Values: values}
		}
	}
	return description.Text{}
}

func namedValue(index projectIndex, named *types.Named, as types.Type, position token.Pos, open []typeKey) reading[valueRead] {
	object := named.Obj()
	switch {
	case packagePath(object) == "time" && object.Name() == "Time":
		return readAs(valueAs(description.Instant{}, as, serverplan.InstantShape{}), nothingFound())
	case packagePath(object) == serverplan.OptionPath && object.Name() == "Option":
		held := named.TypeArgs().At(0)
		return mapped(readValue(index, held, held, position, open), func(inner valueRead) valueRead {
			return valueAs(description.Nullable{Inner: inner.schema}, as, serverplan.NullableShape{Inner: inner.body})
		})
	}
	if fields, isStruct := named.Underlying().(*types.Struct); isStruct {
		return namedStruct(index, named, fields, as, position, open)
	}
	if basic, isBasic := named.Underlying().(*types.Basic); isBasic && basic.Info()&types.IsString != 0 {
		if values := stringConstants(named); len(values) > 0 {
			literals := description.Literals{Values: values}
			return readAs(valueAs(literals, as, serverplan.LiteralShape{Literals: literals}), nothingFound())
		}
	}
	return readValue(index, named.Underlying(), as, position, open)
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
func namedStruct(index projectIndex, named *types.Named, fields *types.Struct, as types.Type, position token.Pos, open []typeKey) reading[valueRead] {
	object := named.Obj()
	key := typeKeyFor(object)
	switch {
	case named.TypeParams().Len() > 0 || named.TypeArgs().Len() > 0:
		return notRead[valueRead](index.refused(position, GenericMember{Member: object.Name()}))
	case slices.Contains(open, key):
		return notRead[valueRead](index.refused(position, RecursiveStruct{Struct: object.Name()}))
	}
	return andThen(structFields(index, fields, append(slices.Clone(open), key)), func(read []fieldWritten) reading[valueRead] {
		whole := structRead(foundStruct{key: key, position: index.placed(object.Pos()), named: description.NamedStruct{Name: object.Name(), Fields: describedFields(read)}})
		return readAs(valueAs(description.StructRef{Name: object.Name()}, as, serverplan.StructShape{Fields: bodyFields(read)}), whole)
	})
}
