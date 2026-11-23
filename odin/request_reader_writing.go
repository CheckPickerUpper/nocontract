package odin

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

func (writer *bindingWriter) inputReader(position int, binding endpointBinding) {
	requestName, receivedName, parameterName := "_", "_", "_"
	if len(binding.endpoint.Query) > 0 || len(binding.endpoint.Headers) > 0 {
		requestName = "request"
	}
	switch binding.endpoint.Body.(type) {
	case description.JSONBody:
		receivedName = "received"
	case description.NoBody, nil:
	}
	for _, segment := range binding.endpoint.Path {
		switch segment.(type) {
		case description.PathParameter:
			parameterName = "parameters"
		case description.PathText, nil:
		}
	}
	fmt.Fprintf(writer.source, "\n@(private, require_results)\nnc_input_%d :: proc(%s: nocontract.Request, %s: nocontract.Received_Request, %s: map[string]string) -> nocontract.Reading(%s) {\n", position, requestName, receivedName, parameterName, writer.nativeType(binding.native.input))
	if len(binding.endpoint.Query) > 0 {
		writer.source.WriteString(" query := nocontract.read_query_parameters(request.query)\n")
	}
	outcome.ForEachOption(compiler.Select[compiler.Struct](writer.base(binding.native.input)), func(input compiler.Struct) {
		writer.inputFields(binding, input.Fields, 0, []string{})
		if len(input.Fields) > 0 {
			writer.source.WriteString(" unreachable()\n")
		}
	})
	writer.source.WriteString("}\n")
}

func (writer *bindingWriter) inputFields(binding endpointBinding, fields []compiler.Field, position int, values []string) {
	if position == len(fields) {
		fmt.Fprintf(writer.source, " return nocontract.Read_Value(%s){value = %s{%s}}\n", writer.nativeType(binding.native.input), writer.nativeType(binding.native.input), strings.Join(values, ", "))
		return
	}
	field := fields[position]
	built := append(slices.Clone(values), fmt.Sprintf("%s = value_%d.value", field.Name, position))
	if field.Name == "body" {
		switch binding.endpoint.Body.(type) {
		case description.JSONBody:
			fmt.Fprintf(writer.source, " switch body in nocontract.start_request_body(received.body) {\n case nocontract.Body_Started:\n parser := body.parser\n switch value_%d in nc_read_%d(&parser, \"body\") {\n case nocontract.Read_Value(%s):\n switch finished in nocontract.finish_body(&parser) {\n case nocontract.Read_Value(nocontract.Body_Finished):\n", position, field.Type, writer.nativeType(field.Type))
			writer.inputFields(binding, fields, position+1, built)
			fmt.Fprintf(writer.source, " case nocontract.Request_Refusal: return finished\n }\n case nocontract.Request_Refusal: return value_%d\n }\n case nocontract.Request_Refusal: return body\n }\n", position)
			return
		case description.NoBody, nil:
		}
	}
	for _, segment := range binding.endpoint.Path {
		switch segment := segment.(type) {
		case description.PathParameter:
			if segment.Field.Name.Text() == field.Name {
				fmt.Fprintf(writer.source, " switch value_%d in nc_parameter_%d(nocontract.Parameter_Text{part = .Path, name = %s, text = parameters[%s]}) {\n case nocontract.Read_Value(%s):\n", position, field.Type, strconv.Quote(field.Name), strconv.Quote(field.Name), writer.nativeType(field.Type))
				writer.inputFields(binding, fields, position+1, built)
				fmt.Fprintf(writer.source, " case nocontract.Request_Refusal: return value_%d\n }\n", position)
				return
			}
		case description.PathText, nil:
		}
	}
	for _, parameter := range binding.endpoint.Query {
		if parameter.Name.Text() == field.Name {
			writer.inputParameter(binding, fields, position, built, "nocontract.query_parameter(query, "+strconv.Quote(field.Name)+")")
			return
		}
	}
	for _, header := range binding.endpoint.Headers {
		if header.Field.Text() == field.Name {
			writer.inputParameter(binding, fields, position, built, "nocontract.header_parameter(request.headers, "+strconv.Quote(header.Name.Text())+")")
			return
		}
	}
	writer.refuse(field.Name, "the checked endpoint contains no source for this native input field")
}

func (writer *bindingWriter) inputParameter(binding endpointBinding, fields []compiler.Field, position int, built []string, expression string) {
	field := fields[position]
	fmt.Fprintf(writer.source, " switch parameter_%d in %s {\n case nocontract.Read_Value(nocontract.Parameter_Text):\n switch value_%d in nc_parameter_%d(parameter_%d.value) {\n case nocontract.Read_Value(%s):\n", position, expression, position, field.Type, position, writer.nativeType(field.Type))
	writer.inputFields(binding, fields, position+1, built)
	fmt.Fprintf(writer.source, " case nocontract.Request_Refusal: return value_%d\n }\n case nocontract.Request_Refusal: return parameter_%d\n }\n", position, position)
}
