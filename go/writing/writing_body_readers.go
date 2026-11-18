package writing

import (
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// bodyReaders is the function the generated file declares to read each struct its routes' bodies
// hold, one for each struct type, every name those functions and the declarations beside them
// take, and their text, each struct's following the readers of the structs its fields hold.
type bodyReaders struct {
	structs []bodyStruct
	taken   []string
	text    *strings.Builder
}

// bodyStruct is one struct a body holds: its Go type, its fields, the function reading it, the
// name that function is given after, which names the readers of unnamed structs its fields hold,
// and the struct's type as the generated file writes it.
type bodyStruct struct {
	goType  types.Type
	fields  []serverplan.BodyStructField
	name    string
	place   string
	written structWritten
}

// structWritten is how the generated file writes a struct's type.
//
//sumtype:decl
type structWritten interface{ isStructWritten() }

// namedStructWritten is a struct a named type declares, written by that type's name.
//
//ergo:not-an-error
type namedStructWritten struct{ name string }

// unnamedStructWritten is an unnamed struct, written by the alias the generated file declares for
// it beside its reader.
//
//ergo:not-an-error
type unnamedStructWritten struct{ alias string }

func (namedStructWritten) isStructWritten()   {}
func (unnamedStructWritten) isStructWritten() {}

// typeName is the struct's type as the generated file writes it.
func (read bodyStruct) typeName() string {
	switch written := read.written.(type) {
	case namedStructWritten:
		return written.name
	case unnamedStructWritten:
		return written.alias
	case nil:
	}
	return ""
}

// structNames is the name of a struct's reader and of the declarations beside it: the alias of an
// unnamed struct, the members the reader keeps, and the function reading one member.
func structNames(name string) []string {
	return []string{name, name + "Struct", name + "Members", name + "Member"}
}

// written is value's Go type as the generated file writes it, an unnamed struct by its alias.
// place is where in a route's body value is held, which names the reader of an unnamed struct.
func (readers *bodyReaders) written(value serverplan.BodyValue, place string, names *imports) string {
	switch shape := value.Shape.(type) {
	case serverplan.StructShape:
		return readers.structRead(value.GoType, shape.Fields, place, names).typeName()
	case serverplan.TextShape, serverplan.WholeShape, serverplan.NumberShape, serverplan.TruthShape, serverplan.InstantShape,
		serverplan.LiteralShape, serverplan.ListShape, serverplan.ArrayShape, serverplan.DictionaryShape, serverplan.NullableShape, nil:
	}
	return names.goType(value.GoType)
}

// reader is the expression reading value, a function of a nocontract.BodyValue. place is where in a
// route's body value is held, which names the reader of an unnamed struct.
func (readers *bodyReaders) reader(value serverplan.BodyValue, place string, names *imports) string {
	goType := names.goType(value.GoType)
	instantiated := func(reader string) string { return names.runtime(reader) + "[" + goType + "]" }
	switch shape := value.Shape.(type) {
	case serverplan.TextShape:
		return instantiated("BodyText")
	case serverplan.WholeShape:
		return instantiated("Body" + wholeReader(shape.Width))
	case serverplan.NumberShape:
		return instantiated("BodyNumber")
	case serverplan.TruthShape:
		return instantiated("BodyTruth")
	case serverplan.InstantShape:
		return names.runtime("BodyInstant")
	case serverplan.LiteralShape:
		return instantiated("BodyLiteral") + "(" + strings.Join(quotedLiterals(shape.Literals), ", ") + ")"
	case serverplan.ListShape:
		return instantiated("BodyList") + "(" + readers.reader(shape.Item, place+"Item", names) + ")"
	case serverplan.ArrayShape:
		item := readers.written(shape.Item, place+"Item", names)
		return fmt.Sprintf("%s(%d, %s, func(items []%s) %s { return %s(items) })", instantiated("BodyArray"), shape.Length,
			readers.reader(shape.Item, place+"Item", names), item, goType, goType)
	case serverplan.DictionaryShape:
		return instantiated("BodyDictionary") + "(" + keyReader(shape, names) + ", " + readers.reader(shape.Value, place+"Value", names) + ")"
	case serverplan.NullableShape:
		return names.runtime("BodyNullable") + "[" + readers.written(shape.Inner, place, names) + "](" + readers.reader(shape.Inner, place, names) + ")"
	case serverplan.StructShape:
		return readers.structRead(value.GoType, shape.Fields, place, names).name
	case nil:
	}
	return ""
}

// keyReader is the expression reading each key of the dictionary shape reads, a function of a
// nocontract.BodyMember: any text, or one of the key's literals.
func keyReader(shape serverplan.DictionaryShape, names *imports) string {
	keyType := names.goType(shape.KeyType)
	switch key := shape.Key.(type) {
	case description.Literals:
		return names.runtime("BodyLiteralKey") + "[" + keyType + "](" + strings.Join(quotedLiterals(key), ", ") + ")"
	case description.Text, nil:
	}
	return names.runtime("BodyTextKey") + "[" + keyType + "]"
}

// structRead is the reader of a struct of goType holding fields, written once for each struct type:
// named after the named type declaring the struct, or after place for an unnamed struct.
func (readers *bodyReaders) structRead(goType types.Type, fields []serverplan.BodyStructField, place string, names *imports) bodyStruct {
	for _, known := range readers.structs {
		if types.Identical(known.goType, goType) {
			return known
		}
	}
	switch typed := goType.(type) {
	case *types.Named:
		return readers.declared(goType, fields, namedPlace(typed, names), func(string) structWritten { return namedStructWritten{name: names.goType(typed)} }, names)
	case *types.Basic, *types.Alias, *types.Struct, *types.Slice, *types.Array, *types.Map, *types.Pointer, *types.Chan, *types.Signature,
		*types.Interface, *types.Tuple, *types.TypeParam, *types.Union, nil:
	}
	return readers.declared(goType, fields, place, func(name string) structWritten { return unnamedStructWritten{alias: name + "Struct"} }, names)
}

// declared is the reader of a struct of goType holding fields, named after place, whose type the
// generated file writes as written gives for the reader's name. Its text is written once its
// fields' readers are named, after the text of every reader it names first.
func (readers *bodyReaders) declared(goType types.Type, fields []serverplan.BodyStructField, place string, written func(name string) structWritten,
	names *imports,
) bodyStruct {
	base := "nocontractBody" + place
	name := base
	for suffix := 2; slices.ContainsFunc(structNames(name), func(each string) bool { return slices.Contains(readers.taken, each) }); suffix++ {
		name = base + strconv.Itoa(suffix)
	}
	readers.taken = append(readers.taken, structNames(name)...)
	read := bodyStruct{goType: goType, fields: fields, name: name, place: place, written: written(name)}
	readers.structs = append(readers.structs, read)
	readers.text.WriteString(readers.structText(read, names))
	return read
}

// namedPlace is the name the reader of a struct named declares is given after: the type's name,
// after its package's alias when another package declares it.
func namedPlace(named *types.Named, names *imports) string {
	return outcome.FoldOption(outcome.EmptyableToOption(named.Obj().Pkg()), func(declaring *types.Package) string {
		switch reference := names.reference(declaring.Path()).(type) {
		case qualifiedBy:
			return exported(reference.alias) + named.Obj().Name()
		case unqualified, nil:
		}
		return named.Obj().Name()
	}, func() string { return named.Obj().Name() })
}

// structText is the function reading one struct, and the declarations beside it: the alias of an
// unnamed struct, the members the object has written so far, each kept until every member is read,
// and the function reading one member, which refuses a member written twice and a member the
// struct does not declare.
func (readers *bodyReaders) structText(read bodyStruct, names *imports) string {
	goType := read.typeName()
	members, member := read.name+"Members", read.name+"Member"
	reading := func(value string) string { return names.runtime("BodyReading") + "[" + value + "]" }
	option := func(function string) string {
		return names.qualified(serverplan.GoName{Package: serverplan.OptionPath, Name: function})
	}
	fieldTypes, starts, kept := []string{}, []string{}, []string{}
	for _, field := range read.fields {
		fieldType := readers.written(field.Value, read.place+field.GoField, names)
		fieldTypes = append(fieldTypes, fieldType)
		starts = append(starts, field.GoField+": "+option("None")+"["+fieldType+"]()")
		kept = append(kept, field.GoField+": "+readName(field.GoField))
	}
	text := &strings.Builder{}
	switch written := read.written.(type) {
	case unnamedStructWritten:
		fmt.Fprintf(text, "\n// %s is the struct %s reads.\ntype %s = %s\n", written.alias, read.name, written.alias, names.goType(read.goType))
	case namedStructWritten, nil:
	}
	fmt.Fprintf(text, "\n// %s holds each member %s has read.\ntype %s struct {\n", members, read.name, members)
	for position, field := range read.fields {
		fmt.Fprintf(text, "%s %s[%s]\n", field.GoField, option("Option"), fieldTypes[position])
	}
	text.WriteString("}\n")
	fmt.Fprintf(text, "\n// %s reads a JSON object holding each field of the struct it returns once.\nfunc %s(value %s) %s {\n", read.name, read.name,
		names.runtime("BodyValue"), reading(goType))
	fmt.Fprintf(text, "return %s(%s(value, %s{%s}, %s), func(members %s) %s {\n", names.runtime("Next"), names.runtime("BodyObject"), members,
		strings.Join(starts, ", "), member, members, reading(goType))
	for position, field := range read.fields {
		fmt.Fprintf(text, "return %s(%s(value, %s, members.%s), func(%s %s) %s {\n", names.runtime("Next"), names.runtime("Present"), strconv.Quote(field.Name),
			field.GoField, readName(field.GoField), fieldTypes[position], reading(goType))
	}
	fmt.Fprintf(text, "return %s[%s]{Value: %s{%s}}\n", names.runtime("Read"), goType, goType, strings.Join(kept, ", "))
	text.WriteString(strings.Repeat("})\n", len(read.fields)+1))
	text.WriteString("}\n")
	fmt.Fprintf(text, "\n// %s reads one member of the object %s reads.\nfunc %s(members %s, member %s) %s {\n", member, read.name, member, members,
		names.runtime("BodyMember"), reading(members))
	if len(read.fields) > 0 {
		text.WriteString("switch member.Name {\n")
		for position, field := range read.fields {
			held := []string{}
			for _, other := range read.fields {
				held = append(held, other.GoField+": members."+other.GoField)
			}
			held[position] = field.GoField + ": " + option("Some") + "(read)"
			fmt.Fprintf(text, "case %s:\nreturn %s(member, members.%s, %s, func(read %s) %s {\nreturn %s{%s}\n})\n", strconv.Quote(field.Name), names.runtime("Once"),
				field.GoField, readers.reader(field.Value, read.place+field.GoField, names), fieldTypes[position], members, members, strings.Join(held, ", "))
		}
		text.WriteString("}\n")
	}
	fmt.Fprintf(text, "return %s[%s](member)\n}\n", names.runtime("Unknown"), members)
	return text.String()
}

// bodyReadNames is every name readName gives a field of a struct value holds.
func bodyReadNames(value serverplan.BodyValue) []string {
	read := []string{}
	for _, held := range value.Values() {
		switch shape := held.Shape.(type) {
		case serverplan.StructShape:
			for _, field := range shape.Fields {
				read = append(read, readName(field.GoField))
			}
		case serverplan.TextShape, serverplan.WholeShape, serverplan.NumberShape, serverplan.TruthShape, serverplan.InstantShape,
			serverplan.LiteralShape, serverplan.ListShape, serverplan.ArrayShape, serverplan.DictionaryShape, serverplan.NullableShape, nil:
		}
	}
	return read
}
