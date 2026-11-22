// Package effect writes a backend's route description as an Effect HttpApi client.
package effect

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/CheckPickerUpper/nocontract/description"
)

// Render writes the schemas, typed failures and group-local endpoints of api.
func Render(api description.API) []byte {
	prefix := privatePrefix(api)
	file := &strings.Builder{}
	file.WriteString(description.GeneratedHeader)
	fmt.Fprintf(file, "\nimport { Schema as %sSchema } from \"effect\"\n", prefix)
	fmt.Fprintf(file, "import { HttpApi as %[1]sHttpApi, HttpApiEndpoint as %[1]sHttpApiEndpoint, HttpApiGroup as %[1]sHttpApiGroup, HttpApiSchema as %[1]sHttpApiSchema } from \"effect/http-api\"\n", prefix)
	for _, named := range api.Structs {
		file.WriteString(namedStructText(named, prefix))
	}
	for _, member := range api.Members {
		file.WriteString(memberText(member, prefix))
	}
	groups := []string{}
	for i, group := range api.Groups {
		name := fmt.Sprintf("%sGroup%d", prefix, i)
		groups = append(groups, name)
		fmt.Fprintf(file, "\nconst %s = %sHttpApiGroup.make(%s)", name, prefix, quoted(group.Name))
		endpoints := []string{}
		for _, endpoint := range group.Endpoints {
			endpoints = append(endpoints, endpointText(endpoint, prefix))
		}
		if len(endpoints) > 0 {
			fmt.Fprintf(file, ".add(\n%s\n)", strings.Join(endpoints, ",\n"))
		}
		file.WriteString("\n")
	}
	fmt.Fprintf(file, "\nexport class %s extends %sHttpApi.make(%s)", api.Name.Text(), prefix, quoted(api.Name.Text()))
	if len(groups) > 0 {
		fmt.Fprintf(file, ".add(%s)", strings.Join(groups, ", "))
	}
	file.WriteString(" {}\n")
	return []byte(file.String())
}

// privatePrefix keeps imports and generated bindings outside the public declarations' names.
func privatePrefix(api description.API) string {
	names := []string{api.Name.Text()}
	for _, member := range api.Members {
		names = append(names, member.Name)
	}
	for _, named := range api.Structs {
		names = append(names, named.Name)
	}
	prefix := "$"
	for _, name := range names {
		for strings.HasPrefix(name, prefix) {
			prefix += "$"
		}
	}
	return prefix
}

func memberText(member description.Member, prefix string) string {
	switch answer := member.Answer.(type) {
	case description.Succeeds:
		fields := schemaFields(member.Fields, prefix, "")
		codec := fmt.Sprintf("%sSchema.TaggedStruct(%s, %s)", prefix, quoted(member.Name), fields.codec)
		return fmt.Sprintf("\nexport const %s = %s.pipe(%sHttpApiSchema.status(%d))\nexport type %s = typeof %s.Type\n", member.Name, codec, prefix, answer.Status.Code(), member.Name, member.Name)
	case description.Fails:
		fields := append([]description.Field{{Name: description.CodeField(), Schema: description.Literals{Values: []string{answer.Code.String()}}}}, member.Fields...)
		return fmt.Sprintf("\nexport class %s extends %sSchema.TaggedError<%s>()(%s, %s, { httpApiStatus: %d }) {}\n", member.Name, prefix, member.Name, quoted(member.Name), schemaFields(fields, prefix, "").codec, answer.Status.Code())
	case nil:
	}
	return ""
}

type renderedPath struct {
	path   string
	params []description.Parameter
}

func renderPath(segments []description.PathSegment) renderedPath {
	parts, params := []string{}, []description.Parameter{}
	for _, segment := range segments {
		switch part := segment.(type) {
		case description.PathText:
			parts = append(parts, part.Text)
		case description.PathParameter:
			parts = append(parts, ":"+part.Field.Name.Text())
			params = append(params, part.Field)
		case nil:
		}
	}
	return renderedPath{path: "/" + strings.Join(parts, "/"), params: params}
}

func endpointText(endpoint description.Endpoint, prefix string) string {
	path := renderPath(endpoint.Path)
	options := []string{}
	if len(path.params) > 0 {
		options = append(options, "params: "+parameterFields(path.params, prefix))
	}
	if len(endpoint.Query) > 0 {
		options = append(options, "query: "+parameterFields(endpoint.Query, prefix))
	}
	switch body := endpoint.Body.(type) {
	case description.JSONBody:
		// A schema keeps the body JSON; a field list would select form encoding.
		options = append(options, "payload: "+renderSchema(body.Schema, prefix, "    ").codec)
	case description.NoBody, nil:
	}
	options = append(options, "success: ["+strings.Join(memberNames(endpoint.Successes.Members()), ", ")+"]", "error: ["+strings.Join(memberNames(endpoint.Failures), ", ")+"]")
	return fmt.Sprintf("  %sHttpApiEndpoint.%s(%s, %s, {\n    %s,\n  })", prefix, strings.ToLower(endpoint.Method.Name()), quoted(endpoint.Name), quoted(path.path), strings.Join(options, ",\n    "))
}

func parameterFields(parameters []description.Parameter, prefix string) string {
	fields := []description.Field{}
	for _, parameter := range parameters {
		fields = append(fields, description.Field{Name: parameter.Name, Schema: parameter.Schema})
	}
	return schemaFields(fields, prefix, "    ").codec
}

func memberNames(members []description.Member) []string {
	names := []string{}
	for _, member := range members {
		names = append(names, member.Name)
	}
	return names
}

// quoted uses the standard JavaScript string escaper for text from backend declarations.
func quoted(text string) string { return "\"" + template.JSEscapeString(text) + "\"" }
