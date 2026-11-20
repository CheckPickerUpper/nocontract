package native

import (
	"encoding/json"
	"net/http"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

type readInput func(*http.Request, bodyObject, func(bodyObject) nocontract.Answer) nocontract.Answer

type readParameter func(nocontract.Text, func(json.RawMessage) nocontract.Answer) nocontract.Answer

func routeInput(route description.Endpoint, bodies bodyReaders) readInput {
	inputs := []readInput{}
	for _, segment := range route.Path {
		switch segment := segment.(type) {
		case description.PathParameter:
			inputs = append(inputs, parameterInput(segment.Field, nocontract.Path))
		case description.PathText, nil:
		}
	}
	for _, field := range route.Query {
		inputs = append(inputs, parameterInput(field, nocontract.Query))
	}
	switch body := route.Body.(type) {
	case description.JSONBody:
		read := compiledBody(bodies, body.Schema)
		inputs = append(inputs, func(request *http.Request, fields bodyObject, next func(bodyObject) nocontract.Answer) nocontract.Answer {
			return nocontract.Then(nocontract.Body(request, read), func(held json.RawMessage) nocontract.Answer {
				fields["body"] = held
				return next(fields)
			})
		})
	case description.NoBody, nil:
	}
	return func(request *http.Request, fields bodyObject, next func(bodyObject) nocontract.Answer) nocontract.Answer {
		return collectInputs(inputs, request, fields, next)
	}
}

func collectInputs(inputs []readInput, request *http.Request, fields bodyObject, next func(bodyObject) nocontract.Answer) nocontract.Answer {
	if len(inputs) == 0 {
		return next(fields)
	}
	return inputs[0](request, fields, func(fields bodyObject) nocontract.Answer {
		return collectInputs(inputs[1:], request, fields, next)
	})
}

func parameterInput(field description.Parameter, part func(*http.Request, string) nocontract.Text) readInput {
	name := field.Name.Text()
	read := parameterReader(field.Schema)
	return func(request *http.Request, fields bodyObject, next func(bodyObject) nocontract.Answer) nocontract.Answer {
		return read(part(request, name), func(held json.RawMessage) nocontract.Answer {
			fields[name] = held
			return next(fields)
		})
	}
}

func encodedParameter[V any](read func(nocontract.Text) nocontract.Reading[V]) readParameter {
	return func(text nocontract.Text, next func(json.RawMessage) nocontract.Answer) nocontract.Answer {
		return nocontract.Then(read(text), func(held V) nocontract.Answer {
			return outcome.Fold(outcome.TupleToResult(json.Marshal(held)), func(encoded []byte) nocontract.Answer {
				return next(json.RawMessage(encoded))
			}, func(cause error) nocontract.Answer { return unwrittenAnswer("request parameter", cause) })
		})
	}
}

func parameterReader(schema description.ParameterSchema) readParameter {
	switch schema := schema.(type) {
	case description.Text:
		return encodedParameter(nocontract.Textual[string])
	case description.Integer:
		return wholeParameter(schema.Width)
	case description.Truth:
		return encodedParameter(nocontract.Truth[bool])
	case description.Literals:
		return encodedParameter(func(text nocontract.Text) nocontract.Reading[string] {
			return nocontract.Literal(text, schema.Values...)
		})
	case nil:
	}
	return unreadParameter()
}

func wholeParameter(width description.IntegerWidth) readParameter {
	switch width.(type) {
	case description.Signed8:
		return encodedParameter(nocontract.Signed[int8])
	case description.Signed16:
		return encodedParameter(nocontract.Signed[int16])
	case description.Signed32:
		return encodedParameter(nocontract.Signed[int32])
	case description.Signed64:
		return encodedParameter(nocontract.Signed[int64])
	case description.Unsigned8:
		return encodedParameter(nocontract.Unsigned[uint8])
	case description.Unsigned16:
		return encodedParameter(nocontract.Unsigned[uint16])
	case description.Unsigned32:
		return encodedParameter(nocontract.Unsigned[uint32])
	case description.Unsigned64:
		return encodedParameter(nocontract.Unsigned[uint64])
	case nil:
	}
	return unreadParameter()
}
