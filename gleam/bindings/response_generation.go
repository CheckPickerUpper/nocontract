package bindings

import (
	"fmt"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
)

func sharedResponses(generated generator, failures []description.SharedFailure) string {
	written := ""
	for _, failure := range failures {
		arguments, fields := []string{}, []string{"#(\"_tag\", json.string(" + quoted(failure.Name) + "))", "#(\"code\", json.string(" + quoted(failure.Code.String()) + "))"}
		for _, field := range failure.Fields {
			name := field.Name.Text()
			typeName, encoder := sharedField(generated, field.Schema, name)
			arguments = append(arguments, name+" "+name+": "+typeName)
			fields = append(fields, "#("+quoted(name)+", "+encoder+")")
		}
		written += fmt.Sprintf("fn shared_%s(%s) -> Answer { let response = json.object([%s]) |> json.to_string |> wisp.json_response(%d) Answer(response:, code: option.Some(%s)) }\n", snakeName(failure.Name), strings.Join(arguments, ", "), strings.Join(fields, ", "), failure.Status.Code(), quoted(failure.Code.String()))
	}
	return written + responseFunctions
}

func sharedField(generated generator, schema description.Schema, name string) (string, string) {
	switch typed := schema.(type) {
	case description.Text, description.Literals:
		return "String", "json.string(" + name + ")"
	case description.Integer:
		return "Int", generated.encoder(Whole{Width: typed.Width}) + "(" + name + ")"
	case description.List:
		return "List(String)", "json.array(" + name + ", of: json.string)"
	case description.Finite, description.Truth, description.Instant, description.FixedList, description.Dictionary, description.Nullable, description.StructRef, description.InlineStruct, nil:
	}
	return "", ""
}

func snakeName(name string) string {
	words := []rune{}
	for index, letter := range name {
		if index > 0 && letter >= 'A' && letter <= 'Z' {
			words = append(words, '_')
		}
		words = append(words, letter)
	}
	return strings.ToLower(string(words))
}

const responseFunctions = `
fn refuse_method(method method: http.Method, allowed allowed: List(String)) -> Answer {
  case method {
    http.Options -> {
      let response = 204 |> wisp.response |> response.set_header("allow", string.join(allowed, ", "))
      Answer(response:, code: option.None)
    }
    http.Get | http.Head | http.Post | http.Put | http.Patch | http.Delete | http.Connect | http.Trace | http.Other(_) -> {
      let refusal = shared_method_not_declared(method: http.method_to_string(method), allowed:)
      Answer(..refusal, response: response.set_header(refusal.response, "allow", string.join(allowed, ", ")))
    }
  }
}

fn request_refused(refusal: request_input.RequestRefusal) -> Answer {
  let #(part, name, reason) = request_input.describe(refusal)
  shared_request_refused(part:, name:, reason:)
}

fn write_answer(tag tag: String, status status: Int, code code: option.Option(String), fields fields: json.Json, schema schema: wire.Schema) -> Answer {
  case fields |> json.to_string |> wire.read_body(schema, record_schemas()) {
    Ok(prepared) -> {
      let response = prepared |> wire.encode_value |> json.to_string |> wisp.json_response(status)
      Answer(response:, code:)
    }
    Error(cause) -> {
      let #(_, reason) = wire.describe(cause)
      shared_answer_not_written(answer: tag, reason:)
    }
  }
}
`

const queryFunction = `
fn query_text(request: wisp.Request) -> String {
  case request.query { option.Some(text) -> text option.None -> "" }
}
`

func queryTextFunction(api description.API) string {
	for _, group := range api.Groups {
		for _, route := range group.Endpoints {
			if len(route.Query) > 0 {
				return queryFunction
			}
		}
	}
	return ""
}
