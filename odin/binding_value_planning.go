package odin

import (
	"slices"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

// valueUse selects which checked values need a generated body reader, parameter reader or encoder.
//
//sumtype:decl
type valueUse interface {
	claim(writer *bindingWriter, id int) bool
}

//ergo:not-an-error
type bodyInput struct{}

//ergo:not-an-error
type answerOutput struct{}

//ergo:not-an-error
type textParameter struct{}

func (bodyInput) claim(writer *bindingWriter, id int) bool    { return claimValue(&writer.from, id) }
func (answerOutput) claim(writer *bindingWriter, id int) bool { return claimValue(&writer.to, id) }
func (textParameter) claim(writer *bindingWriter, id int) bool {
	return claimValue(&writer.parameters, id)
}

func claimValue(ids *[]int, id int) bool {
	if slices.Contains(*ids, id) {
		return false
	}
	*ids = append(*ids, id)
	return true
}

func (writer *bindingWriter) collectEndpoint(binding endpointBinding) {
	writer.nativeType(binding.native.input)
	outcome.ForEachOption(compiler.Select[compiler.Struct](writer.base(binding.native.input)), func(input compiler.Struct) {
		for _, field := range input.Fields {
			for _, segment := range binding.endpoint.Path {
				switch segment := segment.(type) {
				case description.PathParameter:
					if field.Name == segment.Field.Name.Text() {
						writer.collect(field.Type, segment.Field.Schema, textParameter{})
					}
				case description.PathText, nil:
				}
			}
			for _, parameter := range binding.endpoint.Query {
				if field.Name == parameter.Name.Text() {
					writer.collect(field.Type, parameter.Schema, textParameter{})
				}
			}
			for _, header := range binding.endpoint.Headers {
				if field.Name == header.Field.Text() {
					writer.collect(field.Type, header.Schema, textParameter{})
				}
			}
			switch body := binding.endpoint.Body.(type) {
			case description.JSONBody:
				if field.Name == "body" {
					writer.collect(field.Type, body.Schema, bodyInput{})
				}
			case description.NoBody, nil:
			}
		}
	})
	writer.collectMembers(binding.native.output)
	outcome.ForEachOption(binding.native.guard, func(guard guardBinding) { writer.collectMembers(guard.output) })
}

func (writer *bindingWriter) collectMembers(union compiler.Union) {
	for _, variant := range union.Variants {
		outcome.ForEachOption(compiler.Select[compiler.Named](writer.graph.Types[variant]), func(named compiler.Named) {
			for _, member := range writer.api.Members {
				if named.Name == member.Name {
					writer.collect(variant, description.InlineStruct{Fields: member.Fields}, answerOutput{})
				}
			}
		})
	}
}

func (writer *bindingWriter) collect(id int, schema description.Schema, use valueUse) {
	if !use.claim(writer, id) {
		return
	}
	writer.schemas[id] = schema
	writer.nativeType(id)
	switch described := schema.(type) {
	case description.StructRef, description.InlineStruct:
		for position, field := range writer.fields(schema) {
			outcome.ForEachOption(compiler.Select[compiler.Struct](writer.base(id)), func(native compiler.Struct) {
				writer.collect(native.Fields[position].Type, field.Schema, use)
			})
		}
	case description.List:
		outcome.ForEachOption(compiler.Select[compiler.Sequence](writer.base(id)), func(native compiler.Sequence) { writer.collect(native.Element, described.Item, use) })
	case description.FixedList:
		outcome.ForEachOption(compiler.Select[compiler.Sequence](writer.base(id)), func(native compiler.Sequence) { writer.collect(native.Element, described.Item, use) })
	case description.Dictionary:
		outcome.ForEachOption(compiler.Select[compiler.Map](writer.base(id)), func(native compiler.Map) {
			writer.collect(native.Element, described.Value, use)
			switch use.(type) {
			case answerOutput:
				switch key := described.Key.(type) {
				case description.Text:
					writer.collect(native.Key, key, use)
				case description.Literals:
					writer.collect(native.Key, key, use)
				case nil:
				}
			case bodyInput, textParameter, nil:
			}
		})
	case description.Nullable:
		outcome.ForEachOption(compiler.Select[compiler.Union](writer.base(id)), func(native compiler.Union) { writer.collect(native.Variants[0], described.Inner, use) })
	case description.Text, description.Integer, description.Finite, description.Truth, description.Instant, description.Literals:
	case nil:
		writer.unsupported(id, "checked schema traversal")
	}
}

func (writer *bindingWriter) fields(schema description.Schema) []description.Field {
	switch described := schema.(type) {
	case description.InlineStruct:
		return described.Fields
	case description.StructRef:
		for _, record := range writer.api.Structs {
			if record.Name == described.Name {
				return record.Fields
			}
		}
		writer.refuse(described.Name, "the checked record reference has no fields")
	case description.Text, description.Integer, description.Finite, description.Truth, description.Instant, description.Literals,
		description.List, description.FixedList, description.Dictionary, description.Nullable, nil:
	}
	return []description.Field{}
}
