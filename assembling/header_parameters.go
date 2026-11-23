package assembling

import (
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/outcome"
)

// DeclaredHeader keeps a written HTTP header, its native field and schema, and its source together.
type DeclaredHeader struct {
	Name     string
	Field    description.Field
	Position Position
}

// HeaderNameRepeated is a header read twice under names equivalent by HTTP's casing rules.
//
//ergo:code NC-1156
//ergo:message The header {Name} on {Field} is already read as {FirstName} by {FirstField} at {FirstPosition}.
type HeaderNameRepeated struct {
	Name          string
	Field         string
	FirstName     string
	FirstField    string
	FirstPosition string
}

// HeaderFieldRepeated is one native field bound to two different header names.
//
//ergo:code NC-1157
//ergo:message The input field {Field} reads header {Name} and already reads {FirstName} at {FirstPosition}.
type HeaderFieldRepeated struct {
	Field         string
	Name          string
	FirstName     string
	FirstPosition string
}

// HeaderBindingConflict is an input field assigned a header and another request location.
//
//ergo:code NC-1158
//ergo:message The input field {Field} reads header {Name} and also reads {Part} {Binding}.
type HeaderBindingConflict struct {
	Name    string
	Field   string
	Part    string
	Binding string
}

// HeaderFieldUndeclared is a header directive naming no public field of the route's native input.
//
//ergo:code NC-1159
//ergo:message The header {Name} names input field {Field}, which is not one of the public input fields {Available}.
type HeaderFieldUndeclared struct {
	Name      string
	Field     string
	Available string
}

type headerCheck struct {
	parameters []description.HeaderParameter
	refusals   []Refusal
}

func checkedHeaders(declared []DeclaredHeader) headerCheck {
	parameters, refusals := []description.HeaderParameter{}, []Refusal{}
	names, fields := map[description.HeaderName]DeclaredHeader{}, map[description.FieldName]DeclaredHeader{}
	for _, header := range declared {
		switch parsed := description.ParseHeaderName(header.Name).(type) {
		case description.HeaderNameRead:
			if first, repeated := names[parsed.Name]; repeated {
				refusals = append(refusals, Refusal{Position: header.Position, Problem: HeaderNameRepeated{
					Name: header.Name, Field: header.Field.Name.Text(), FirstName: first.Name,
					FirstField: first.Field.Name.Text(), FirstPosition: first.Position.String(),
				}})
				continue
			}
			names[parsed.Name] = header
			if first, repeated := fields[header.Field.Name]; repeated {
				refusals = append(refusals, Refusal{Position: header.Position, Problem: HeaderFieldRepeated{
					Field: header.Field.Name.Text(), Name: header.Name, FirstName: first.Name, FirstPosition: first.Position.String(),
				}})
				continue
			}
			fields[header.Field.Name] = header
			outcome.FoldOption(description.ParseParameter(header.Field), func(parameter description.Parameter) struct{} {
				parameters = append(parameters, description.HeaderParameter{Name: parsed.Name, Field: parameter.Name, Schema: parameter.Schema})
				return struct{}{}
			}, func() struct{} {
				refusals = append(refusals, Refusal{Position: header.Position, Problem: parameterNotReadable(header.Field, "header")})
				return struct{}{}
			})
		case description.HeaderNameRefused:
			refusals = append(refusals, Refusal{Position: header.Position, Problem: parsed.Problem})
		case nil:
		}
	}
	return headerCheck{parameters: parameters, refusals: refusals}
}
