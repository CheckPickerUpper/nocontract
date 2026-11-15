package nocontract

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/description"
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

// Success is a success member as it is answered: its status, its tag and its JSON fields.
type Success struct {
	Status int
	Tag    string
	Fields mo.Result[[]byte]
}

// Failure is a failure member as it is answered: its status, its tag, its permanent code and its
// JSON fields.
type Failure struct {
	Status int
	Tag    string
	Code   string
	Fields mo.Result[[]byte]
}

// Succeeded is a success member's answer.
func Succeeded(success Success) Answer {
	return Answer{status: success.Status, tag: success.Tag, code: mo.None[string](), fields: success.Fields}
}

// Failed is a failure member's answer.
func Failed(failure Failure) Answer {
	return Answer{status: failure.Status, tag: failure.Tag, code: mo.Some(failure.Code), fields: failure.Fields}
}

// NoOutcome is the answer of a route whose handler returned no outcome.
func NoOutcome() Answer {
	return NocontractAnswerSharedFailure(AnswerNotWritten{Answer: "", Reason: "the route answered no outcome"})
}

// fallbackFields are AnswerNotWritten's fields, written by hand for when even they cannot be
// encoded.
const fallbackFields = `{"answer":"","reason":"the failure could not be encoded"}`

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
		failed := NocontractAnswerSharedFailure(AnswerNotWritten{Answer: answer.tag, Reason: cause.Error()})
		fallback := Answer{status: failed.status, tag: failed.tag, code: failed.code, fields: mo.Ok([]byte(fallbackFields))}
		// The fallback's fields are an object, so its body is always written; an empty body is never sent.
		asIs := func(body []byte) []byte { return body }
		writeBody(writer, failed.status, mo.Fold(failed.body(), asIs, func(error) []byte {
			return mo.Fold(fallback.body(), asIs, func(error) []byte { return []byte{} })
		}))
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
		object := bytes.NewBufferString("{")
		object.Write(quoted(description.TagField))
		object.WriteString(":")
		object.Write(quoted(answer.tag))
		answer.code.ForEach(func(code string) {
			object.WriteString(",")
			object.Write(quoted(description.CodeField))
			object.WriteString(":")
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
