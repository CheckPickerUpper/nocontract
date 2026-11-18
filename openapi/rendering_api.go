// Package openapi writes a backend's API description as an OpenAPI 3.1 document: one JSON file
// holding each route's parameters, body and every member it answers with under its status, and
// each member and named struct as a schema, for any other language's client generator and any
// documentation tool.
package openapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

// documentVersion is the OpenAPI version the document is written in.
const documentVersion = "3.1.0"

// schemaPrefix is where a reference finds a schema the document declares.
const schemaPrefix = "#/components/schemas/"

// jsonMediaType is the media type every body is sent as.
const jsonMediaType = "application/json"

// Render is the OpenAPI document of api, whose own version is version, as JSON indented by two
// spaces with a closing newline, or why encoding/json could not write it.
func Render(api description.API, version string) mo.Result[[]byte] {
	tags := []tag{}
	for _, group := range api.Groups {
		tags = append(tags, tag{Name: group.Name})
	}
	written := document{
		OpenAPI:    documentVersion,
		Info:       info{Title: api.Name, Version: version},
		Tags:       tags,
		Paths:      paths(api),
		Components: components{Schemas: componentSchemas(api)},
	}
	return mo.Fold(mo.TupleToResult(json.MarshalIndent(written, "", "  ")), func(encoded []byte) mo.Result[[]byte] {
		return mo.Ok(append(encoded, '\n'))
	}, mo.Err[[]byte])
}

// paths is every route's operation, under its path and its method.
func paths(api description.API) map[string]map[string]operation {
	written := map[string]map[string]operation{}
	for _, group := range api.Groups {
		for _, endpoint := range group.Endpoints {
			path := writtenPath(endpoint.Path)
			methods := mo.Fold(keyed.Entry(written, path), func(found map[string]operation) map[string]operation { return found },
				func(error) map[string]operation { return map[string]operation{} })
			methods[strings.ToLower(endpoint.Method.Name())] = routeOperation(group.Name, endpoint)
			written[path] = methods
		}
	}
	return written
}

// writtenPath is a route's path as OpenAPI writes it, each parameter as {name}.
func writtenPath(segments []description.PathSegment) string {
	parts := []string{}
	for _, segment := range segments {
		switch written := segment.(type) {
		case description.PathText:
			parts = append(parts, written.Text)
		case description.PathParameter:
			parts = append(parts, "{"+written.Field.Name+"}")
		case nil:
		}
	}
	return "/" + strings.Join(parts, "/")
}

// routeOperation is one route of the group named groupName: its group as its tag, an id no other
// route takes, its parameters, its body and its responses.
func routeOperation(groupName string, endpoint description.Endpoint) operation {
	parameters := []parameter{}
	for _, segment := range endpoint.Path {
		switch written := segment.(type) {
		case description.PathParameter:
			parameters = append(parameters, parameter{Name: written.Field.Name, In: "path", Required: true, Schema: fieldSchema(written.Field.Schema)})
		case description.PathText, nil:
		}
	}
	for _, query := range endpoint.Query {
		parameters = append(parameters, parameter{Name: query.Name, In: "query", Required: true, Schema: fieldSchema(query.Schema)})
	}
	tags := []string{groupName}
	// Endpoint names are unique within a group, so the group's name makes the id unique.
	operationID := groupName + "." + endpoint.Name
	answers := responses(endpoint)
	switch body := endpoint.Body.(type) {
	case description.JSONBody:
		return bodyOperation{
			Tags: tags, OperationID: operationID, Parameters: parameters,
			RequestBody: requestBody{Required: true, Content: jsonContent(fieldSchema(body.Schema))},
			Responses:   answers,
		}
	case description.NoBody, nil:
	}
	return bodylessOperation{Tags: tags, OperationID: operationID, Parameters: parameters, Responses: answers}
}

// jsonContent is a body of bodySchema, sent as JSON.
func jsonContent(bodySchema schema) map[string]mediaType {
	return map[string]mediaType{jsonMediaType: {Schema: bodySchema}}
}

// statusMember is a member a route answers with, and the number of the status it answers under.
type statusMember struct {
	status int
	member description.Member
}

// responses is every status a route answers with, each holding the members it answers under that
// status, in the order the route lists them.
func responses(endpoint description.Endpoint) map[string]response {
	answered := []statusMember{}
	for _, member := range slices.Concat(endpoint.Successes, endpoint.Failures) {
		switch answer := member.Answer.(type) {
		case description.Succeeds:
			answered = append(answered, statusMember{status: answer.Status.Code(), member: member})
		case description.Fails:
			answered = append(answered, statusMember{status: answer.Status.Code(), member: member})
		case nil:
		}
	}
	written := map[string]response{}
	for _, first := range keyed.FirstOfEach(answered, func(each statusMember) int { return each.status }) {
		names := []string{}
		for _, each := range answered {
			if each.status == first.status {
				names = append(names, each.member.Name)
			}
		}
		written[strconv.Itoa(first.status)] = response{Description: http.StatusText(first.status), Content: jsonContent(answerSchema(names))}
	}
	return written
}

// answerSchema is the body of a status the members named names answer under: the one member's
// schema, or exactly one of several.
func answerSchema(names []string) schema {
	references := []schema{}
	for _, name := range names {
		references = append(references, reference(name))
	}
	if len(references) == 1 {
		return references[0]
	}
	return oneOfSchema{OneOf: references}
}

func reference(name string) referenceSchema { return referenceSchema{Ref: schemaPrefix + name} }

// componentSchemas is every named struct, and every member as its answer is written: its tag, a
// failure's permanent code, then its fields.
func componentSchemas(api description.API) map[string]schema {
	written := map[string]schema{}
	for _, named := range api.Structs {
		written[named.Name] = structSchema(named.Fields)
	}
	for _, member := range api.Members {
		fields := []description.Field{{Name: description.TagField, Schema: description.Literals{Values: []string{member.Name}}}}
		switch answer := member.Answer.(type) {
		case description.Fails:
			fields = append(fields, description.Field{Name: description.CodeField, Schema: description.Literals{Values: []string{answer.Code.String()}}})
		case description.Succeeds, nil:
		}
		written[member.Name] = structSchema(append(fields, member.Fields...))
	}
	return written
}

// structSchema is an object holding exactly fields, every one of them required.
func structSchema(fields []description.Field) objectSchema {
	properties, required := map[string]schema{}, []string{}
	for _, field := range fields {
		properties[field.Name] = fieldSchema(field.Schema)
		required = append(required, field.Name)
	}
	return objectSchema{Type: "object", Properties: properties, Required: required, AdditionalProperties: false}
}

// fieldSchema is the one JSON Schema of a value's shape.
func fieldSchema(shape description.Schema) schema {
	switch typed := shape.(type) {
	case description.Text:
		return typeSchema{Type: "string"}
	case description.Integer:
		return typeSchema{Type: "integer"}
	case description.Finite:
		return typeSchema{Type: "number"}
	case description.Truth:
		return typeSchema{Type: "boolean"}
	case description.Instant:
		return formatSchema{Type: "string", Format: "date-time"}
	case description.Literals:
		return enumSchema{Type: "string", Enum: append([]string{}, typed.Values...)}
	case description.List:
		return arraySchema{Type: "array", Items: fieldSchema(typed.Item)}
	case description.Dictionary:
		return dictionarySchema{Type: "object", AdditionalProperties: fieldSchema(typed.Value)}
	case description.Nullable:
		return anyOfSchema{AnyOf: []schema{fieldSchema(typed.Inner), typeSchema{Type: "null"}}}
	case description.StructRef:
		return reference(typed.Name)
	case description.InlineStruct:
		return structSchema(typed.Fields)
	case nil:
	}
	return neverSchema{Not: struct{}{}}
}
