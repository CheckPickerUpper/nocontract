package odin

import (
	"fmt"
	"strconv"

	"github.com/CheckPickerUpper/nocontract/description"
)

func (writer *bindingWriter) parameterReader(id int) {
	fmt.Fprintf(writer.source, "\n@(private, require_results)\nnc_parameter_%d :: proc(source: nocontract.Parameter_Text) -> nocontract.Reading(%s) {\n", id, writer.nativeType(id))
	switch schema := writer.schemas[id].(type) {
	case description.Text:
		fmt.Fprintf(writer.source, " return nocontract.Read_Value(%s){value = %s(source.text)}\n", writer.nativeType(id), writer.nativeType(id))
	case description.Truth:
		writer.writeScalarReader(id, scalarReader{expression: "nocontract.read_parameter_truth(source)", valueType: "bool"})
		writer.source.WriteString(" unreachable()\n")
	case description.Integer:
		parser := parserForInteger(schema.Width)
		writer.writeScalarReader(id, scalarReader{expression: fmt.Sprintf("nocontract.read_parameter_%s(source, nocontract.%s{least = %s, most = %s})", parser.name, parser.rangeType, schema.Width.Least(), schema.Width.Most()), valueType: parser.valueType})
		writer.source.WriteString(" unreachable()\n")
	case description.Literals:
		writer.source.WriteString(" switch source.text {\n")
		for _, value := range schema.Values {
			fmt.Fprintf(writer.source, " case %s: return nocontract.Read_Value(%s){value = (%s).%s}\n", strconv.Quote(value), writer.nativeType(id), writer.nativeType(id), value)
		}
		fmt.Fprintf(writer.source, " }\n return nocontract.Request_Refusal(nocontract.Parameter_Refusal{source = source, problem = nocontract.Parameter_Literal_Outside{values = %s}})\n", writer.literalValues(schema.Values))
	case description.Finite, description.Instant, description.List, description.FixedList, description.Dictionary, description.Nullable, description.StructRef, description.InlineStruct, nil:
		writer.unsupported(id, "text parameter reading")
	}
	writer.source.WriteString("}\n")
}
