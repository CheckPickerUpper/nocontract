// Package effect writes a backend's API description as an Effect HttpApi: one TypeScript file
// whose schemas, tagged errors, groups and API class give HttpApiClient.make a typed client. It
// is the one writer of the client, whichever language the backend is written in.
package effect

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
)

// ImportedNames is every name the file imports, which no declaration of the API may take.
func ImportedNames() []string {
	return []string{"Schema", "HttpApi", "HttpApiEndpoint", "HttpApiGroup", "HttpApiSchema"}
}

// identifier is a JSON name a TypeScript object can hold without quotes.
func identifier() *regexp.Regexp { return regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`) }

// Render is the TypeScript file of api.
func Render(api description.API) []byte {
	file := &strings.Builder{}
	file.WriteString(description.GeneratedHeader)
	file.WriteString("\nimport { Schema } from \"effect\"\n")
	fmt.Fprintf(file, "import { %s } from \"effect/http-api\"\n", strings.Join(httpAPIImports(api), ", "))
	for _, named := range api.Structs {
		fmt.Fprintf(file, "\nexport const %s = Schema.Struct(%s)\nexport type %s = typeof %s.Type\n", named.Name, fieldsText(named.Fields, ""), named.Name, named.Name)
	}
	for _, member := range api.Members {
		file.WriteString("\n")
		file.WriteString(memberText(member))
	}
	groupClasses := []string{}
	for _, group := range api.Groups {
		className := description.GroupClassName(group.Name)
		groupClasses = append(groupClasses, className)
		endpoints := []string{}
		for _, endpoint := range group.Endpoints {
			endpoints = append(endpoints, endpointText(endpoint))
		}
		fmt.Fprintf(file, "\nexport class %s extends HttpApiGroup.make(%s).add(\n%s\n) {}\n", className, quoted(group.Name), strings.Join(endpoints, ",\n"))
	}
	if len(groupClasses) > 0 {
		fmt.Fprintf(file, "\nexport class %s extends HttpApi.make(%s).add(%s) {}\n", api.Name, quoted(api.Name), strings.Join(groupClasses, ", "))
		return []byte(file.String())
	}
	fmt.Fprintf(file, "\nexport class %s extends HttpApi.make(%s) {}\n", api.Name, quoted(api.Name))
	return []byte(file.String())
}

// httpAPIImports is what the file imports from effect/http-api: HttpApiSchema only when a success
// answers with a status other than 200, which it annotates.
func httpAPIImports(api description.API) []string {
	imports := []string{"HttpApi", "HttpApiEndpoint", "HttpApiGroup"}
	for _, member := range api.Members {
		switch answer := member.Answer.(type) {
		case description.Succeeds:
			switch answer.Status.Declared().(type) {
			case description.OtherSuccess:
				return append(imports, "HttpApiSchema")
			case description.DefaultSuccess, nil:
			}
		case description.Fails, nil:
		}
	}
	return imports
}

// memberText is a success as a tagged struct with its status, or a failure as a tagged error
// carrying its permanent code and status.
func memberText(member description.Member) string {
	switch answer := member.Answer.(type) {
	case description.Succeeds:
		declared := fmt.Sprintf("Schema.TaggedStruct(%s, %s)", quoted(member.Name), fieldsText(member.Fields, ""))
		switch status := answer.Status.Declared().(type) {
		case description.OtherSuccess:
			declared += fmt.Sprintf(".pipe(HttpApiSchema.status(%d))", status.Code)
		case description.DefaultSuccess, nil:
		}
		return fmt.Sprintf("export const %s = %s\nexport type %s = typeof %s.Type\n", member.Name, declared, member.Name, member.Name)
	case description.Fails:
		fields := append([]description.Field{{Name: description.CodeField, Schema: description.Literals{Values: []string{answer.Code.String()}}}}, member.Fields...)
		return fmt.Sprintf("export class %s extends Schema.TaggedError<%s>()(%s, %s, { httpApiStatus: %d }) {}\n",
			member.Name, member.Name, quoted(member.Name), fieldsText(fields, ""), answer.Status.Code())
	case nil:
	}
	return ""
}

// pathText is a route's path as Effect writes it, each parameter as :name, and the fields that
// read its parameters, in path order.
type pathText struct {
	path   string
	params []description.Field
}

func writtenPath(segments []description.PathSegment) pathText {
	parts, params := []string{}, []description.Field{}
	for _, segment := range segments {
		switch written := segment.(type) {
		case description.PathText:
			parts = append(parts, written.Text)
		case description.PathParameter:
			parts = append(parts, ":"+written.Field.Name)
			params = append(params, written.Field)
		case nil:
		}
	}
	return pathText{path: "/" + strings.Join(parts, "/"), params: params}
}

func endpointText(endpoint description.Endpoint) string {
	path := writtenPath(endpoint.Path)
	options := []string{}
	if len(path.params) > 0 {
		options = append(options, "params: "+fieldsText(path.params, "    "))
	}
	if len(endpoint.Query) > 0 {
		options = append(options, "query: "+fieldsText(endpoint.Query, "    "))
	}
	switch body := endpoint.Body.(type) {
	case description.JSONBody:
		// A plain field list would make Effect send a form, so the body is always a schema.
		options = append(options, "payload: "+schemaText(body.Schema, "    "))
	case description.NoBody, nil:
	}
	options = append(options, "success: ["+strings.Join(memberNames(endpoint.Successes), ", ")+"]", "error: ["+strings.Join(memberNames(endpoint.Failures), ", ")+"]")
	return fmt.Sprintf("  HttpApiEndpoint.%s(%s, %s, {\n    %s,\n  })", strings.ToLower(endpoint.Method.Name()), quoted(endpoint.Name), quoted(path.path), strings.Join(options, ",\n    "))
}

// memberNames is the name each member is declared under in the file.
func memberNames(members []description.Member) []string {
	names := []string{}
	for _, member := range members {
		names = append(names, member.Name)
	}
	return names
}

func fieldsText(fields []description.Field, indent string) string {
	if len(fields) == 0 {
		return "{}"
	}
	lines := []string{}
	for _, field := range fields {
		name := field.Name
		if !identifier().MatchString(name) {
			name = quoted(name)
		}
		lines = append(lines, fmt.Sprintf("%s  %s: %s,", indent, name, schemaText(field.Schema, indent+"  ")))
	}
	return "{\n" + strings.Join(lines, "\n") + "\n" + indent + "}"
}

func schemaText(schema description.Schema, indent string) string {
	switch typed := schema.(type) {
	case description.Text:
		return "Schema.String"
	case description.Integer:
		return "Schema.Int"
	case description.Finite:
		return "Schema.Finite"
	case description.Truth:
		return "Schema.Boolean"
	case description.Instant:
		return "Schema.DateTimeUtcFromString"
	case description.Literals:
		values := []string{}
		for _, value := range typed.Values {
			values = append(values, quoted(value))
		}
		if len(values) == 1 {
			return "Schema.Literal(" + values[0] + ")"
		}
		return "Schema.Literals([" + strings.Join(values, ", ") + "])"
	case description.List:
		return "Schema.Array(" + schemaText(typed.Item, indent) + ")"
	case description.Dictionary:
		return "Schema.Record(Schema.String, " + schemaText(typed.Value, indent) + ")"
	case description.Nullable:
		return "Schema.NullOr(" + schemaText(typed.Inner, indent) + ")"
	case description.StructRef:
		return typed.Name
	case description.InlineStruct:
		return "Schema.Struct(" + fieldsText(typed.Fields, indent) + ")"
	case nil:
	}
	return "Schema.Never"
}

// quoted is text as a TypeScript string literal, written as a JSON string.
func quoted(text string) string {
	return mo.Fold(mo.TupleToResult(json.Marshal(text)), func(encoded []byte) string { return string(encoded) }, func(error) string { return `""` })
}
