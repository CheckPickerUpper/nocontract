package odin

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

func (writer *bindingWriter) wireType(id int) string {
	switch schema := writer.schemas[id].(type) {
	case description.Text, description.Literals, description.Instant:
		return "string"
	case description.Truth:
		return "bool"
	case description.Integer:
		switch schema.Width.Wire().(type) {
		case description.DecimalTextWire:
			return "string"
		case description.NumberWire:
			return writer.nativeShape(id, writer.base(id))
		case nil:
		}
	case description.Finite:
		return writer.nativeShape(id, writer.base(id))
	case description.StructRef, description.InlineStruct:
		return fmt.Sprintf("nc_wire_%d", id)
	case description.List:
		return outcome.FoldOption(compiler.Select[compiler.Sequence](writer.base(id)), func(native compiler.Sequence) string { return "[]" + writer.wireType(native.Element) }, func() string { return writer.unsupported(id, "wire list") })
	case description.FixedList:
		return outcome.FoldOption(compiler.Select[compiler.Sequence](writer.base(id)), func(native compiler.Sequence) string {
			return fmt.Sprintf("[%d]%s", schema.Length, writer.wireType(native.Element))
		}, func() string { return writer.unsupported(id, "wire fixed list") })
	case description.Dictionary:
		return outcome.FoldOption(compiler.Select[compiler.Map](writer.base(id)), func(native compiler.Map) string { return "map[string]" + writer.wireType(native.Element) }, func() string { return writer.unsupported(id, "wire dictionary") })
	case description.Nullable:
		return outcome.FoldOption(compiler.Select[compiler.Union](writer.base(id)), func(native compiler.Union) string { return "Maybe(" + writer.wireType(native.Variants[0]) + ")" }, func() string { return writer.unsupported(id, "wire nullable") })
	case nil:
	}
	return writer.unsupported(id, "wire type")
}

func (writer *bindingWriter) wireDeclaration(id int) {
	switch schema := writer.schemas[id].(type) {
	case description.StructRef, description.InlineStruct:
		outcome.ForEachOption(compiler.Select[compiler.Struct](writer.base(id)), func(native compiler.Struct) {
			fmt.Fprintf(writer.source, "\n@(private)\nnc_wire_%d :: struct #all_or_none {\n", id)
			for position, field := range writer.fields(schema) {
				fmt.Fprintf(writer.source, " field_%d: %s %sjson:%s%s,\n", position, writer.wireType(native.Fields[position].Type), "\x60", strconv.Quote(field.Name.Text()), "\x60")
			}
			writer.source.WriteString("}\n")
		})
	case description.Text, description.Integer, description.Finite, description.Truth, description.Instant, description.Literals,
		description.List, description.FixedList, description.Dictionary, description.Nullable, nil:
	}
}

func (writer *bindingWriter) toDeclaration(id int) {
	nativeType, wireType := writer.nativeType(id), writer.wireType(id)
	fmt.Fprintf(writer.source, "\n@(private, require_results)\nnc_to_%d :: proc(input: %s, path: string) -> nocontract.Value_Preparation(%s) {\n", id, nativeType, wireType)
	switch schema := writer.schemas[id].(type) {
	case description.Text, description.Truth, description.Finite:
		fmt.Fprintf(writer.source, " _ = path\n return nocontract.Prepared_Value(%s){value = %s(input)}\n", wireType, wireType)
		writer.source.WriteString("}\n")
		return
	case description.Integer:
		writer.source.WriteString(" _ = path\n")
		switch schema.Width.Wire().(type) {
		case description.NumberWire:
			fmt.Fprintf(writer.source, " return nocontract.Prepared_Value(%s){value = %s(input)}\n", wireType, wireType)
		case description.DecimalTextWire:
			writer.source.WriteString(" return nocontract.Prepared_Value(string){value = fmt.tprintf(\"%d\", input)}\n")
		case nil:
		}
		writer.source.WriteString("}\n")
		return
	case description.Instant:
		writer.source.WriteString(" switch instant in nocontract.read_instant(string(input)) {\n case nocontract.UTC_Instant: return nocontract.Prepared_Value(string){value = instant.text}\n case nocontract.Instant_Not_Read: return nocontract.Value_Not_Prepared{path = path, cause = instant}\n }\n")
	case description.Literals:
		writer.source.WriteString(" switch input {\n")
		for _, value := range schema.Values {
			fmt.Fprintf(writer.source, " case .%s: return nocontract.Prepared_Value(string){value = %s}\n", value, strconv.Quote(value))
		}
		fmt.Fprintf(writer.source, " }\n return nocontract.Value_Not_Prepared{path = path, cause = nocontract.Native_Literal_Outside{native_type = %s, read = fmt.tprintf(\"%%v\", input), declared = %s}}\n}\n", nativeType, writer.literalValues(schema.Values))
		return
	case description.StructRef, description.InlineStruct:
		outcome.ForEachOption(compiler.Select[compiler.Struct](writer.base(id)), func(native compiler.Struct) {
			writer.toStruct(id, native, 0, []string{})
			if len(native.Fields) > 0 {
				writer.source.WriteString(" unreachable()\n")
			}
		})
		writer.source.WriteString("}\n")
		return
	case description.Nullable:
		outcome.ForEachOption(compiler.Select[compiler.Union](writer.base(id)), func(native compiler.Union) {
			child := native.Variants[0]
			fmt.Fprintf(writer.source, " switch held in input {\n case nil: return nocontract.Prepared_Value(%s){value = {}}\n case %s:\n switch prepared in nc_to_%d(held, path) {\n case nocontract.Prepared_Value(%s): return nocontract.Prepared_Value(%s){value = prepared.value}\n case nocontract.Value_Not_Prepared: return prepared\n }\n }\n", wireType, writer.nativeType(child), child, writer.wireType(child), wireType)
		})
	case description.List, description.FixedList:
		outcome.ForEachOption(compiler.Select[compiler.Sequence](writer.base(id)), func(native compiler.Sequence) {
			container := "make(" + wireType + ", len(input), context.temp_allocator)"
			switch schema.(type) {
			case description.FixedList:
				container = wireType + "{}"
			case description.List:
			case description.Text, description.Integer, description.Finite, description.Truth, description.Instant, description.Literals, description.Dictionary, description.Nullable, description.StructRef, description.InlineStruct, nil:
			}
			fmt.Fprintf(writer.source, " output := %s\n for value, index in input {\n switch prepared in nc_to_%d(value, nocontract.body_child_path(path, nocontract.List_Item_Step{index = index})) {\n case nocontract.Prepared_Value(%s): output[index] = prepared.value\n case nocontract.Value_Not_Prepared: return prepared\n }\n }\n return nocontract.Prepared_Value(%s){value = output}\n", container, native.Element, writer.wireType(native.Element), wireType)
		})
		writer.source.WriteString("}\n")
		return
	case description.Dictionary:
		outcome.ForEachOption(compiler.Select[compiler.Map](writer.base(id)), func(native compiler.Map) {
			fmt.Fprintf(writer.source, " output := make(%s, context.temp_allocator)\n for key, value in input {\n switch prepared_key in nc_to_%d(key, path) {\n case nocontract.Prepared_Value(string):\n switch prepared_value in nc_to_%d(value, nocontract.body_child_path(path, nocontract.Dictionary_Key_Step{key = prepared_key.value})) {\n case nocontract.Prepared_Value(%s): output[prepared_key.value] = prepared_value.value\n case nocontract.Value_Not_Prepared: return prepared_value\n }\n case nocontract.Value_Not_Prepared: return prepared_key\n }\n }\n return nocontract.Prepared_Value(%s){value = output}\n", wireType, native.Key, native.Element, writer.wireType(native.Element), wireType)
		})
		writer.source.WriteString("}\n")
		return
	case nil:
		writer.unsupported(id, "answer preparation")
	}
	writer.source.WriteString(" unreachable()\n}\n")
}

func (writer *bindingWriter) toStruct(id int, native compiler.Struct, position int, fields []string) {
	if position == len(native.Fields) {
		fmt.Fprintf(writer.source, " return nocontract.Prepared_Value(%s){value = %s{%s}}\n", writer.wireType(id), writer.wireType(id), strings.Join(fields, ", "))
		return
	}
	field := native.Fields[position]
	fmt.Fprintf(writer.source, " switch field_%d in nc_to_%d(input.%s, nocontract.body_child_path(path, nocontract.Object_Field_Step{name = %s})) {\n case nocontract.Prepared_Value(%s):\n", position, field.Type, field.Name, strconv.Quote(writer.fields(writer.schemas[id])[position].Name.Text()), writer.wireType(field.Type))
	writer.toStruct(id, native, position+1, append(slices.Clone(fields), fmt.Sprintf("field_%d = field_%d.value", position, position)))
	fmt.Fprintf(writer.source, " case nocontract.Value_Not_Prepared: return field_%d\n }\n", position)
}

func (writer *bindingWriter) answerDeclaration(id int) {
	outcome.ForEachOption(compiler.Select[compiler.Named](writer.graph.Types[id]), func(named compiler.Named) {
		for _, member := range writer.api.Members {
			if member.Name == named.Name {
				writer.memberResponse(id, member)
			}
		}
	})
}

func (writer *bindingWriter) memberResponse(id int, member description.Member) {
	outcome.ForEachOption(compiler.Select[compiler.Struct](writer.base(id)), func(native compiler.Struct) {
		status, declaration, code, metadataCode := 0, "", "", "{}"
		switch answer := member.Answer.(type) {
		case description.Succeeds:
			status = answer.Status.Code()
		case description.Fails:
			status = answer.Status.Code()
			declaration = "code: string,"
			code = "code = " + strconv.Quote(answer.Code.String()) + ","
			metadataCode = strconv.Quote(answer.Code.String())
		case nil:
			writer.unsupported(id, "member status")
		}
		fields, values := []string{}, []string{}
		for position, field := range member.Fields {
			fields = append(fields, fmt.Sprintf("field_%d: %s %sjson:%s%s,", position, writer.wireType(native.Fields[position].Type), "\x60", strconv.Quote(field.Name.Text()), "\x60"))
			values = append(values, fmt.Sprintf("field_%d = prepared.value.field_%d,", position, position))
		}
		fmt.Fprintf(writer.source, "\n@(private, require_results)\nnc_answer_%d :: proc(answer: %s) -> nocontract.Answer {\n switch prepared in nc_to_%d(answer, \"$\") {\n case nocontract.Prepared_Value(%s):\n value := struct #all_or_none {_tag: string, %s %s}{_tag = %s, %s %s}\n return nc_encode_answer(value, nc_Answer_Metadata{tag = %s, status = %d, code = %s})\n case nocontract.Value_Not_Prepared:\n return nc_shared_AnswerNotWritten(nc_fields_AnswerNotWritten{answer = %s, reason = fmt.tprintf(\"%%v\", prepared)}, {})\n }\n unreachable()\n}\n", id, writer.nativeType(id), id, writer.wireType(id), declaration, strings.Join(fields, " "), strconv.Quote(member.Name), code, strings.Join(values, " "), strconv.Quote(member.Name), status, metadataCode, strconv.Quote(member.Name))
	})
}

func (writer *bindingWriter) sharedResponses() {
	// The crash answer is written whole before dispatch by crashResponse.
	crashed := description.RequestCrashed().Failure.Code
	for _, failure := range description.SharedFailures() {
		if failure.Code == crashed {
			continue
		}
		fields, wireFields, values := []string{}, []string{}, []string{}
		for _, field := range failure.Fields {
			fieldType := ""
			wireType, encoded := "", "fields."+field.Name.Text()
			switch schema := field.Schema.(type) {
			case description.Text, description.Literals:
				fieldType = "string"
			case description.Integer:
				fieldType = sharedIntegerName(schema.Width)
				switch schema.Width.Wire().(type) {
				case description.DecimalTextWire:
					wireType = "string"
					encoded = "fmt.tprintf(\"%d\", fields." + field.Name.Text() + ")"
				case description.NumberWire, nil:
				}
			case description.List:
				fieldType = "[]string"
			case description.Finite, description.Truth, description.Instant, description.FixedList, description.Dictionary, description.Nullable, description.StructRef, description.InlineStruct, nil:
				writer.refuse(failure.Name+"."+field.Name.Text(), "shared failure has no Odin field representation")
			}
			fields = append(fields, field.Name.Text()+": "+fieldType+",")
			if wireType == "" {
				wireType = fieldType
			}
			wireFields = append(wireFields, field.Name.Text()+": "+wireType+",")
			values = append(values, field.Name.Text()+" = "+encoded+",")
		}
		fmt.Fprintf(writer.source, "\n@(private)\nnc_fields_%s :: struct #all_or_none {%s}\n@(private, require_results)\nnc_shared_%s :: proc(fields: nc_fields_%s, allowed: []string) -> nocontract.Answer {\n value := struct #all_or_none {_tag: string, code: string, %s}{_tag = %s, code = %s, %s}\n switch encoded in nocontract.encode_json(value) {\n case nocontract.Encoded_JSON: return nocontract.Answer{body = nocontract.JSON_Answer{status = %d, code = %s, encoded = encoded}, allowed = allowed}\n case nocontract.JSON_Not_Encoded(type_of(value)):\n log.errorf(\"NoContract could not encode its shared failure: %%v\", encoded)\n return nocontract.Answer{body = %s, allowed = allowed}\n }\n unreachable()\n}\n", failure.Name, strings.Join(fields, " "), failure.Name, failure.Name, strings.Join(wireFields, " "), strconv.Quote(failure.Name), strconv.Quote(failure.Code.String()), strings.Join(values, " "), failure.Status.Code(), strconv.Quote(failure.Code.String()), writer.encodingFailure(failure.Name))
	}
}

func sharedIntegerName(width description.IntegerWidth) string {
	switch width.(type) {
	case description.Signed8:
		return "i8"
	case description.Signed16:
		return "i16"
	case description.Signed32:
		return "i32"
	case description.Signed64:
		return "i64"
	case description.Unsigned8:
		return "u8"
	case description.Unsigned16:
		return "u16"
	case description.Unsigned32:
		return "u32"
	case description.Unsigned64:
		return "u64"
	case nil:
	}
	return ""
}

func (writer *bindingWriter) encodingFailure(answer string) string {
	for _, failure := range description.SharedFailures() {
		if failure.Name != "AnswerNotWritten" {
			continue
		}
		fields := map[string]string{"_tag": failure.Name, "code": failure.Code.String()}
		for _, field := range failure.Fields {
			switch field.Name.Text() {
			case "answer":
				fields[field.Name.Text()] = answer
			case "reason":
				fields[field.Name.Text()] = "NoContract could not encode its shared failure."
			}
		}
		return outcome.Fold(outcome.TupleToResult(json.Marshal(fields)), func(encoded []byte) string {
			return fmt.Sprintf("nocontract.JSON_Answer{status = %d, code = %s, encoded = nocontract.Encoded_JSON{bytes = transmute([]u8)string(%s)}}", failure.Status.Code(), strconv.Quote(failure.Code.String()), strconv.Quote(string(encoded)))
		}, func(cause error) string { writer.problems = append(writer.problems, cause); return "" })
	}
	return writer.refuse("AnswerNotWritten", "the shared failure table contains no encoding failure")
}
