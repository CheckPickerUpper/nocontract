package odin

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

type scalarReader struct {
	expression string
	valueType  string
}

type integerParser struct {
	name      string
	valueType string
	rangeType string
}

func parserForInteger(width description.IntegerWidth) integerParser {
	switch width.(type) {
	case description.Signed8, description.Signed16, description.Signed32, description.Signed64:
		return integerParser{name: "signed", valueType: "i64", rangeType: "Signed_Integer_Range"}
	case description.Unsigned8, description.Unsigned16, description.Unsigned32, description.Unsigned64:
		return integerParser{name: "unsigned", valueType: "u64", rangeType: "Unsigned_Integer_Range"}
	case nil:
	}
	return integerParser{name: "", valueType: "", rangeType: ""}
}

func integerEncoding(width description.IntegerWidth) string {
	switch width.Wire().(type) {
	case description.NumberWire:
		return ".JSON_Number"
	case description.DecimalTextWire:
		return ".Decimal_String"
	case nil:
	}
	return ""
}

func (writer *bindingWriter) bodyReader(id int) {
	writer.coreImport("encoding/json")
	fmt.Fprintf(writer.source, "\n@(private, require_results)\nnc_read_%d :: proc(parser: ^json.Parser, path: string) -> nocontract.Reading(%s) {\n", id, writer.nativeType(id))
	switch schema := writer.schemas[id].(type) {
	case description.Text:
		writer.writeScalarReader(id, scalarReader{expression: "nocontract.read_body_text(parser, path)", valueType: "string"})
	case description.Truth:
		writer.writeScalarReader(id, scalarReader{expression: "nocontract.read_body_truth(parser, path)", valueType: "bool"})
	case description.Instant:
		writer.writeScalarReader(id, scalarReader{expression: "nocontract.read_body_instant(parser, path)", valueType: "string"})
	case description.Integer:
		parser := parserForInteger(schema.Width)
		writer.writeScalarReader(id, scalarReader{expression: fmt.Sprintf("nocontract.read_body_%s(parser, path, %s, nocontract.%s{least = %s, most = %s})", parser.name, integerEncoding(schema.Width), parser.rangeType, schema.Width.Least(), schema.Width.Most()), valueType: parser.valueType})
	case description.Finite:
		writer.coreImport("math")
		fmt.Fprintf(writer.source, " original := parser.curr_token\n switch read in nocontract.read_body_number(parser, path) {\n case nocontract.Read_Value(f64):\n value := %s(read.value)\n if math.is_inf(f64(value)) || math.is_nan(f64(value)) { return nocontract.Request_Refusal(nocontract.Body_Problem{path = path, kind = nocontract.Body_Value_Mistyped{want = \"a finite number\", read = nocontract.Read_Text{value = original.text}}}) }\n return nocontract.Read_Value(%s){value = value}\n case nocontract.Request_Refusal: return read\n }\n", writer.nativeType(id), writer.nativeType(id))
	case description.Literals:
		writer.source.WriteString(" switch read in nocontract.read_body_text(parser, path) {\n case nocontract.Read_Value(string):\n switch read.value {\n")
		for _, value := range schema.Values {
			fmt.Fprintf(writer.source, " case %s: return nocontract.Read_Value(%s){value = (%s).%s}\n", strconv.Quote(value), writer.nativeType(id), writer.nativeType(id), value)
		}
		fmt.Fprintf(writer.source, " }\n return nocontract.Request_Refusal(nocontract.Body_Problem{path = path, kind = nocontract.Body_Literal_Outside{read = nocontract.Read_Text{value = read.value}, values = %s}})\n case nocontract.Request_Refusal: return read\n }\n", writer.literalValues(schema.Values))
	case description.StructRef, description.InlineStruct:
		outcome.ForEachOption(compiler.Select[compiler.Struct](writer.base(id)), func(native compiler.Struct) { writer.bodyStructReader(id, native) })
	case description.List, description.FixedList:
		outcome.ForEachOption(compiler.Select[compiler.Sequence](writer.base(id)), func(native compiler.Sequence) { writer.bodyListReader(id, native) })
	case description.Dictionary:
		outcome.ForEachOption(compiler.Select[compiler.Map](writer.base(id)), func(native compiler.Map) { writer.bodyDictionaryReader(id, native, schema) })
	case description.Nullable:
		outcome.ForEachOption(compiler.Select[compiler.Union](writer.base(id)), func(native compiler.Union) {
			child := native.Variants[0]
			writer.source.WriteString(" switch parser.curr_token.kind {\n case .Null:\n switch read in nocontract.consume_body_token(parser, path) {\n")
			fmt.Fprintf(writer.source, " case nocontract.Read_Value(json.Token): return nocontract.Read_Value(%s){value = {}}\n case nocontract.Request_Refusal: return read\n }\n", writer.nativeType(id))
			writer.source.WriteString(" case .Invalid, .EOF, .False, .True, .Infinity, .NaN, .Ident, .Integer, .Float, .String, .Colon, .Comma, .Open_Brace, .Close_Brace, .Open_Bracket, .Close_Bracket:\n")
			fmt.Fprintf(writer.source, " switch read in nc_read_%d(parser, path) {\n case nocontract.Read_Value(%s): return nocontract.Read_Value(%s){value = read.value}\n case nocontract.Request_Refusal: return read\n }\n }\n", child, writer.nativeType(child), writer.nativeType(id))
		})
	case nil:
		writer.unsupported(id, "body reading")
	}
	writer.source.WriteString(" unreachable()\n}\n")
}

func (writer *bindingWriter) writeScalarReader(id int, read scalarReader) {
	fmt.Fprintf(writer.source, " switch read in %s {\n case nocontract.Read_Value(%s): return nocontract.Read_Value(%s){value = %s(read.value)}\n case nocontract.Request_Refusal: return read\n }\n", read.expression, read.valueType, writer.nativeType(id), writer.nativeType(id))
}

func (writer *bindingWriter) bodyStructReader(id int, native compiler.Struct) {
	fields := writer.fields(writer.schemas[id])
	for position, field := range native.Fields {
		fmt.Fprintf(writer.source, " field_%d: nocontract.Body_Field(%s) = nocontract.Field_Not_Read{}\n", position, writer.nativeType(field.Type))
	}
	writer.source.WriteString(" switch opened in nocontract.begin_body_container(parser, path, .Object) {\n case nocontract.Read_Value(nocontract.Container_Progress):\n progress := opened.value\n for progress == .More {\n switch key in nocontract.read_body_member(parser, path) {\n case nocontract.Read_Value(string):\n child_path := nocontract.body_child_path(path, nocontract.Object_Field_Step{name = key.value})\n switch key.value {\n")
	for position, field := range fields {
		nativeField := native.Fields[position]
		fmt.Fprintf(writer.source, " case %s:\n switch _ in field_%d {\n case nocontract.Field_Not_Read:\n case nocontract.Read_Value(%s): return nocontract.Request_Refusal(nocontract.Body_Problem{path = child_path, kind = nocontract.Body_Member_Repeated{}})\n }\n switch read in nc_read_%d(parser, child_path) {\n case nocontract.Read_Value(%s): field_%d = read\n case nocontract.Request_Refusal: return read\n }\n", strconv.Quote(field.Name.Text()), position, writer.nativeType(nativeField.Type), nativeField.Type, writer.nativeType(nativeField.Type), position)
	}
	writer.source.WriteString(" case: return nocontract.Request_Refusal(nocontract.Body_Problem{path = child_path, kind = nocontract.Body_Member_Unknown{}})\n }\n case nocontract.Request_Refusal: return key\n }\n switch next in nocontract.continue_body_container(parser, path, .Object) {\n case nocontract.Read_Value(nocontract.Container_Progress): progress = next.value\n case nocontract.Request_Refusal: return next\n }\n }\n")
	writer.completeBodyStruct(id, native, fields, 0, []string{})
	writer.source.WriteString(" case nocontract.Request_Refusal: return opened\n }\n")
}

func (writer *bindingWriter) completeBodyStruct(id int, native compiler.Struct, fields []description.Field, position int, values []string) {
	if position == len(native.Fields) {
		fmt.Fprintf(writer.source, " return nocontract.Read_Value(%s){value = %s{%s}}\n", writer.nativeType(id), writer.nativeType(id), strings.Join(values, ", "))
		return
	}
	field := native.Fields[position]
	fmt.Fprintf(writer.source, " switch value_%d in field_%d {\n case nocontract.Read_Value(%s):\n", position, position, writer.nativeType(field.Type))
	writer.completeBodyStruct(id, native, fields, position+1, append(append([]string{}, values...), fmt.Sprintf("%s = value_%d.value", field.Name, position)))
	fmt.Fprintf(writer.source, " case nocontract.Field_Not_Read: return nocontract.Request_Refusal(nocontract.Body_Problem{path = nocontract.body_child_path(path, nocontract.Object_Field_Step{name = %s}), kind = nocontract.Body_Member_Missing{}})\n }\n", strconv.Quote(fields[position].Name.Text()))
}

func (writer *bindingWriter) bodyListReader(id int, native compiler.Sequence) {
	element := writer.nativeType(native.Element)
	fmt.Fprintf(writer.source, " values := make([dynamic]%s, context.temp_allocator)\n switch opened in nocontract.begin_body_container(parser, path, .List) {\n case nocontract.Read_Value(nocontract.Container_Progress):\n progress := opened.value\n for progress == .More {\n switch read in nc_read_%d(parser, nocontract.body_child_path(path, nocontract.List_Item_Step{index = len(values)})) {\n case nocontract.Read_Value(%s): append(&values, read.value)\n case nocontract.Request_Refusal: return read\n }\n switch next in nocontract.continue_body_container(parser, path, .List) {\n case nocontract.Read_Value(nocontract.Container_Progress): progress = next.value\n case nocontract.Request_Refusal: return next\n }\n }\n", element, native.Element, element)
	switch schema := writer.schemas[id].(type) {
	case description.FixedList:
		fmt.Fprintf(writer.source, " if len(values) != %d { return nocontract.Request_Refusal(nocontract.Body_Problem{path = path, kind = nocontract.Body_List_Length_Wrong{wanted = %d, read = len(values)}}) }\n", schema.Length, schema.Length)
		fmt.Fprintf(writer.source, " return nocontract.Read_Value(%s){value = %s{", writer.nativeType(id), writer.nativeType(id))
		for index := range schema.Length {
			fmt.Fprintf(writer.source, "values[%d],", index)
		}
		writer.source.WriteString("}}\n")
	case description.List:
		switch native.Storage.(type) {
		case compiler.Slice:
			fmt.Fprintf(writer.source, " return nocontract.Read_Value(%s){value = values[:]}\n", writer.nativeType(id))
		case compiler.Dynamic:
			fmt.Fprintf(writer.source, " return nocontract.Read_Value(%s){value = values}\n", writer.nativeType(id))
		case compiler.Fixed, nil:
			writer.unsupported(id, "list storage")
		}
	case description.Text, description.Integer, description.Finite, description.Truth, description.Instant, description.Literals, description.Dictionary,
		description.Nullable, description.StructRef, description.InlineStruct, nil:
		writer.unsupported(id, "list schema")
	}
	writer.source.WriteString(" case nocontract.Request_Refusal: return opened\n }\n")
}

func (writer *bindingWriter) bodyDictionaryReader(id int, native compiler.Map, schema description.Dictionary) {
	fmt.Fprintf(writer.source, " values := make(%s, context.temp_allocator)\n switch opened in nocontract.begin_body_container(parser, path, .Object) {\n case nocontract.Read_Value(nocontract.Container_Progress):\n progress := opened.value\n for progress == .More {\n switch key in nocontract.read_body_member(parser, path) {\n case nocontract.Read_Value(string):\n child_path := nocontract.body_child_path(path, nocontract.Dictionary_Key_Step{key = key.value})\n", writer.nativeType(id))
	switch key := schema.Key.(type) {
	case description.Text:
		fmt.Fprintf(writer.source, " native_key := %s(key.value)\n", writer.nativeType(native.Key))
		writer.bodyDictionaryValue(native)
	case description.Literals:
		writer.source.WriteString(" switch key.value {\n")
		for _, value := range key.Values {
			fmt.Fprintf(writer.source, " case %s:\n native_key := (%s).%s\n", strconv.Quote(value), writer.nativeType(native.Key), value)
			writer.bodyDictionaryValue(native)
		}
		fmt.Fprintf(writer.source, " case: return nocontract.Request_Refusal(nocontract.Body_Problem{path = child_path, kind = nocontract.Body_Key_Outside{read = nocontract.Read_Text{value = key.value}, keys = %s}})\n }\n", writer.literalValues(key.Values))
	case nil:
		writer.unsupported(id, "dictionary key")
	}
	fmt.Fprintf(writer.source, " case nocontract.Request_Refusal: return key\n }\n switch next in nocontract.continue_body_container(parser, path, .Object) {\n case nocontract.Read_Value(nocontract.Container_Progress): progress = next.value\n case nocontract.Request_Refusal: return next\n }\n }\n return nocontract.Read_Value(%s){value = values}\n case nocontract.Request_Refusal: return opened\n }\n", writer.nativeType(id))
}

func (writer *bindingWriter) bodyDictionaryValue(native compiler.Map) {
	fmt.Fprintf(writer.source, " if native_key in values { return nocontract.Request_Refusal(nocontract.Body_Problem{path = child_path, kind = nocontract.Body_Member_Repeated{}}) }\n switch read in nc_read_%d(parser, child_path) {\n case nocontract.Read_Value(%s): values[native_key] = read.value\n case nocontract.Request_Refusal: return read\n }\n", native.Element, writer.nativeType(native.Element))
}

func odinStrings(values []string) string {
	quoted := []string{}
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[]string{" + strings.Join(quoted, ", ") + "}"
}
