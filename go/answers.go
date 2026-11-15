package nocontract

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/samber/mo"
)

// Answer is what a route writes: its member's status, its member's Go type name as the tag the
// Effect client tells members apart by, a failure's permanent code, and the member's JSON fields.
// Generated code builds it with Succeeded or Failed.
type Answer struct {
	status int
	tag    string
	code   mo.Option[string]
	fields mo.Result[[]byte]
}

// Succeeded is a success member's answer: its status, its tag and its JSON fields.
func Succeeded(status int, tag string, fields mo.Result[[]byte]) Answer {
	return Answer{status: status, tag: tag, code: mo.None[string](), fields: fields}
}

// Failed is a failure member's answer: its status, its tag, its permanent code and its fields.
func Failed(status int, tag, code string, fields mo.Result[[]byte]) Answer {
	return Answer{status: status, tag: tag, code: mo.Some(code), fields: fields}
}

// NoOutcome is the answer of a route whose handler returned no outcome.
func NoOutcome() Answer {
	return AnswerNotWritten{Answer: "", Reason: "the route answered no outcome"}.answer()
}

// answerFallback is written only when even AnswerNotWritten cannot be encoded.
const answerFallback = `{"_tag":"AnswerNotWritten","code":"NC-1002","answer":"","reason":"the failure could not be encoded"}`

// FieldsNotObject is an answer whose JSON fields are not an object, so it has nowhere to hold its
// tag.
//
//ergo:code NC-1003
type FieldsNotObject struct{ Tag string }

func (refused FieldsNotObject) Error() string {
	return "nocontract: the answer " + refused.Tag + " is not written as a JSON object"
}

// Write writes the answer with its status, as one JSON object holding its tag, a failure's code
// and its fields, or AnswerNotWritten when its fields cannot be written.
func Write(writer http.ResponseWriter, answer Answer) {
	mo.Fold(answer.body(), func(body []byte) struct{} {
		writeBody(writer, answer.status, body)
		return struct{}{}
	}, func(cause error) struct{} {
		failed := AnswerNotWritten{Answer: answer.tag, Reason: cause.Error()}.answer()
		writeBody(writer, failed.status, mo.Fold(failed.body(), func(body []byte) []byte { return body }, func(error) []byte { return []byte(answerFallback) }))
		return struct{}{}
	})
}

func writeBody(writer http.ResponseWriter, status int, body []byte) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	// The status is already sent, so a body the client stopped reading has no one left to tell.
	_, _ = writer.Write(body)
}

// body is the answer's JSON object: its tag, a failure's code, then its own fields.
func (answer Answer) body() mo.Result[[]byte] {
	return mo.Fold(answer.fields, func(fields []byte) mo.Result[[]byte] {
		if !bytes.HasPrefix(fields, []byte("{")) || !bytes.HasSuffix(fields, []byte("}")) {
			return mo.Err[[]byte](FieldsNotObject{Tag: answer.tag})
		}
		object := bytes.NewBufferString(`{"_tag":`)
		object.Write(quoted(answer.tag))
		answer.code.ForEach(func(code string) {
			object.WriteString(`,"code":`)
			object.Write(quoted(code))
		})
		if inner := bytes.TrimSpace(fields[1 : len(fields)-1]); len(inner) > 0 {
			object.WriteString(",")
			object.Write(inner)
		}
		object.WriteString("}")
		return mo.Ok(object.Bytes())
	}, mo.Err[[]byte])
}

// quoted is text as a JSON string. Marshalling a Go string cannot fail.
func quoted(text string) []byte {
	return mo.Fold(mo.TupleToResult(json.Marshal(text)), func(encoded []byte) []byte { return encoded }, func(error) []byte { return []byte(`""`) })
}
