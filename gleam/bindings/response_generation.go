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
		records := []string{}
		for _, field := range failure.Fields {
			name := field.Name.Text()
			shared := sharedField(generated, field.Schema, name, failure.Name+pascalName(name))
			arguments = append(arguments, name+" "+name+": "+shared.typeName)
			fields = append(fields, "#("+quoted(name)+", "+shared.encoder+")")
			records = append(records, shared.records...)
		}
		for _, record := range records {
			written += record + "\n"
		}
		written += fmt.Sprintf("fn shared_%s(%s) -> Answer { let response = json.object([%s]) |> json.to_string |> wisp.json_response(%d) Answer(response:, code: option.Some(%s)) }\n", snakeName(failure.Name), strings.Join(arguments, ", "), strings.Join(fields, ", "), failure.Status.Code(), quoted(failure.Code.String()))
	}
	return written + responseFunctions
}

// sharedGleamField is a shared failure field's Gleam type, the expression encoding the value
// bound to its name, and the record types an inline object of it declares.
type sharedGleamField struct {
	typeName string
	encoder  string
	records  []string
}

// sharedField writes one shared failure field. An inline object becomes a labelled record named
// record, so a runtime builds it by its field names.
func sharedField(generated generator, schema description.Schema, name string, record string) sharedGleamField {
	switch typed := schema.(type) {
	case description.Text, description.Literals:
		return sharedGleamField{typeName: "String", encoder: "json.string(" + name + ")", records: []string{}}
	case description.Integer:
		return sharedGleamField{typeName: "Int", encoder: generated.encoder(Whole{Width: typed.Width}) + "(" + name + ")", records: []string{}}
	case description.List:
		return sharedGleamField{typeName: "List(String)", encoder: "json.array(" + name + ", of: json.string)", records: []string{}}
	case description.Nullable:
		inner := name + "_value"
		held := sharedField(generated, typed.Inner, inner, record)
		return sharedGleamField{typeName: "option.Option(" + held.typeName + ")", encoder: "case " + name + " { option.Some(" + inner + ") -> " + held.encoder + " option.None -> json.null() }", records: held.records}
	case description.InlineStruct:
		labels, bindings, fields, records := []string{}, []string{}, []string{}, []string{}
		for _, field := range typed.Fields {
			label := field.Name.Text()
			binding := name + "_" + label
			member := sharedField(generated, field.Schema, binding, record+pascalName(label))
			labels = append(labels, label+": "+member.typeName)
			bindings = append(bindings, label+": "+binding)
			fields = append(fields, "#("+quoted(label)+", "+member.encoder+")")
			records = append(records, member.records...)
		}
		declared := "type " + record + " { " + record + "(" + strings.Join(labels, ", ") + ") }"
		return sharedGleamField{
			typeName: record,
			encoder:  "case " + name + " { " + record + "(" + strings.Join(bindings, ", ") + ") -> json.object([" + strings.Join(fields, ", ") + "]) }",
			records:  append(records, declared),
		}
	case description.Finite, description.Truth, description.Instant, description.FixedList, description.Dictionary, description.StructRef, nil:
	}
	return sharedGleamField{typeName: "", encoder: "", records: []string{}}
}

// crashAnswerFunction builds RequestCrashed from the protected request's route, naming each
// field of the shared failure's route record by identity.
func crashAnswerFunction() string {
	crash := description.RequestCrashed()
	route := crash.Route.Text()
	return fmt.Sprintf(`
fn crash_answer(evidence: recovery.RequestEvidence) -> Answer {
  let %[1]s = case evidence.route {
    recovery.RouteMatched(method:, path:) -> option.Some(%[2]s(%[3]s: method, %[4]s: path))
    recovery.NoRouteMatched -> option.None
  }
  shared_%[5]s(%[1]s:)
}

`, route, crash.Failure.Name+pascalName(route), crash.RouteMethod.Text(), crash.RoutePath.Text(), snakeName(crash.Failure.Name))
}

// pascalName is a snake_case wire name as a Gleam type name.
func pascalName(name string) string {
	words := strings.Split(name, "_")
	for index, word := range words {
		if word != "" {
			words[index] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, "")
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
