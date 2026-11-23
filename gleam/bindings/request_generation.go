package bindings

import (
	"fmt"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
)

func (generated generator) inputReader(name string, input Record, endpoint description.Endpoint) string {
	lines, arguments := []string{}, []string{}
	inputs := map[string]string{}
	for _, segment := range endpoint.Path {
		switch segment := segment.(type) {
		case description.PathParameter:
			field := segment.Field
			inputs[field.Name.Text()] = "request_input.read_path(parameters, " + quoted(field.Name.Text()) + ", " + generated.parameterSchema(field.Schema) + ", "
		case description.PathText, nil:
		}
	}
	for _, field := range endpoint.Query {
		generated.module("gleam/option")
		inputs[field.Name.Text()] = "request_input.read_query(target.read_query(query_text(request)), " + quoted(field.Name.Text()) + ", " + generated.parameterSchema(field.Schema) + ", "
	}
	for _, header := range endpoint.Headers {
		inputs[header.Field.Text()] = "request_input.read_header(request.headers, " + quoted(header.Name.Text()) + ", " + generated.parameterSchema(header.Schema) + ", "
	}
	switch body := endpoint.Body.(type) {
	case description.JSONBody:
		inputs["body"] = "request_input.read_body(request, " + generated.schema(body.Schema) + ", record_schemas(), "
	case description.NoBody, nil:
	}
	for index, field := range input.Fields {
		generated.module("gleam/result")
		local := fmt.Sprintf("field_%d", index)
		lines = append(lines, "use "+local+" <- result.try("+inputs[field.Name.Text()]+generated.decoder(field.Value)+"))")
		arguments = append(arguments, local)
	}
	request, parameters := "_request", "_parameters"
	if len(endpoint.Query) > 0 || len(endpoint.Headers) > 0 {
		request = "request"
	}
	switch endpoint.Body.(type) {
	case description.JSONBody:
		request = "request"
	case description.NoBody, nil:
	}
	for _, segment := range endpoint.Path {
		if _, found := segment.(description.PathParameter); found {
			parameters = "parameters"
		}
	}
	lines = append(lines, "Ok("+constructorCall(generated.module(input.Native.Identity.Module), input.Constructor, arguments)+")")
	return "fn " + name + "(" + request + ": wisp.Request, " + parameters + ": dict.Dict(String, String)) -> Result(" + generated.typeName(input.Native) + ", request_input.RequestRefusal) {\n" + strings.Join(lines, "\n") + "\n}\n"
}

func pathSegments(segments []description.PathSegment) string {
	written := []string{}
	for _, segment := range segments {
		switch segment := segment.(type) {
		case description.PathText:
			written = append(written, "target.PathText(text: "+quoted(segment.Text)+")")
		case description.PathParameter:
			written = append(written, "target.PathParameter(name: "+quoted(segment.Field.Name.Text())+")")
		case nil:
		}
	}
	return "[" + strings.Join(written, ", ") + "]"
}

func (generated generator) parameterSchema(schema description.ParameterSchema) string {
	switch schema := schema.(type) {
	case description.Text:
		generated.module("nocontract_runtime/parameter_reading")
		return "parameter.TextParameter"
	case description.Truth:
		generated.module("nocontract_runtime/parameter_reading")
		return "parameter.TruthParameter"
	case description.Literals:
		generated.module("nocontract_runtime/parameter_reading")
		return "parameter.LiteralParameter(values: " + stringList(schema.Values) + ")"
	case description.Integer:
		generated.module("nocontract_runtime/parameter_reading")
		parser := "ParseInt"
		if schema.Width.Least() == "0" {
			parser = "ParseUint"
		}
		return "parameter.IntegerParameter(least: " + schema.Width.Least() + ", most: " + schema.Width.Most() + ", parser: " + quoted(parser) + ")"
	case nil:
	}
	generated.problems["parameter schema"] = SchemaUnresolved{Schema: schema}
	return ""
}

// SchemaUnresolved identifies an assembled shape that has no Gleam runtime representation.
type SchemaUnresolved struct{ Schema description.Schema }

func (failure SchemaUnresolved) Error() string {
	return fmt.Sprintf("Gleam runtime has no schema for %+v", failure.Schema)
}

func (generated generator) schema(schema description.Schema) string {
	switch schema := schema.(type) {
	case description.Text:
		return "wire.TextSchema"
	case description.Truth:
		return "wire.TruthSchema"
	case description.Finite:
		return "wire.FiniteSchema"
	case description.Instant:
		return "wire.InstantSchema"
	case description.Integer:
		encoding := ""
		switch schema.Width.Wire().(type) {
		case description.DecimalTextWire:
			encoding = "wire.DecimalString"
		case description.NumberWire:
			encoding = "wire.JsonNumber"
		case nil:
		}
		return "wire.IntegerSchema(least: " + schema.Width.Least() + ", most: " + schema.Width.Most() + ", encoding: " + encoding + ")"
	case description.Literals:
		return "wire.LiteralsSchema(values: " + stringList(schema.Values) + ")"
	case description.List:
		return "wire.ListSchema(item: " + generated.schema(schema.Item) + ")"
	case description.FixedList:
		return fmt.Sprintf("wire.FixedListSchema(item: %s, length: %d)", generated.schema(schema.Item), schema.Length)
	case description.Nullable:
		return "wire.NullableSchema(inner: " + generated.schema(schema.Inner) + ")"
	case description.Dictionary:
		keys := ""
		switch key := schema.Key.(type) {
		case description.Text:
			keys = "wire.AnyKey"
		case description.Literals:
			keys = "wire.LiteralKeys(values: " + stringList(key.Values) + ")"
		case nil:
		}
		return "wire.DictionarySchema(keys: " + keys + ", item: " + generated.schema(schema.Value) + ")"
	case description.StructRef:
		return "wire.ReferenceSchema(name: " + quoted(schema.Name) + ")"
	case description.InlineStruct:
		return generated.recordSchema(schema.Fields)
	case nil:
	}
	generated.problems["body schema"] = SchemaUnresolved{Schema: schema}
	return ""
}

func (generated generator) recordSchema(fields []description.Field) string {
	written := []string{}
	for _, field := range fields {
		written = append(written, "#("+quoted(field.Name.Text())+", "+generated.schema(field.Schema)+")")
	}
	return "wire.RecordSchema(fields: [" + strings.Join(written, ", ") + "])"
}

func (generated generator) recordSchemas(records []description.NamedStruct) string {
	written := []string{}
	for _, record := range records {
		written = append(written, "#("+quoted(record.Name)+", "+generated.recordSchema(record.Fields)+")")
	}
	return "fn record_schemas() -> dict.Dict(String, wire.Schema) { dict.from_list([" + strings.Join(written, ", ") + "]) }\n"
}

func stringList(values []string) string {
	written := []string{}
	for _, value := range values {
		written = append(written, quoted(value))
	}
	return "[" + strings.Join(written, ", ") + "]"
}
