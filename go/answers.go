package nocontract

// nocontract_routes.go declares the failures nocontract answers on a route's behalf, from their one
// declaration in package description.
//go:generate go run ../internal/cmd/runtime-answers generate

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"net/http"

	"github.com/samber/mo"
)

// Answer is what a route writes: its member's status, its member's Go type name as the tag the
// Effect client tells members apart by, a failure's permanent code, and one JSON object holding
// the tag, the code and the member's own fields. Generated code builds it with Succeeded or Failed.
type Answer struct {
	status int
	tag    string
	code   mo.Option[string]
	body   mo.Result[[]byte]
}

// Success is a success member as it is answered: its status and its tag.
type Success struct {
	Status int
	Tag    string
}

// Failure is a failure member as it is answered: its status, its tag and its permanent code.
type Failure struct {
	Status int
	Tag    string
	Code   string
}

// Succeeded is a success member's answer, member's fields written after its tag.
func Succeeded[M any](success Success, member M) Answer {
	return Answer{status: success.Status, tag: success.Tag, code: mo.None[string](), body: encoded(taggedSuccess[M]{Tag: success.Tag, Fields: member})}
}

// Failed is a failure member's answer, member's fields written after its tag and code.
func Failed[M any](failure Failure, member M) Answer {
	return Answer{
		status: failure.Status, tag: failure.Tag, code: mo.Some(failure.Code),
		body: encoded(taggedFailure[M]{Tag: failure.Tag, Code: failure.Code, Fields: member}),
	}
}

// encoded is value as encoding/json writes it, which writes an embedded member's fields beside the
// tag and code in the one object it encodes.
func encoded[V any](value V) mo.Result[[]byte] {
	return mo.TupleToResult(jsonv2.Marshal(value, json.DefaultOptionsV1()))
}

// NoOutcome is the answer of a route whose handler returned no outcome.
func NoOutcome() Answer {
	return NocontractAnswerSharedFailure(AnswerNotWritten{Answer: "", Reason: "the route answered no outcome"})
}

// sentAnswer is an answer as it is sent: its status, its tag, a failure's code, and one JSON object
// holding its tag, its code and its fields.
type sentAnswer struct {
	status int
	tag    string
	code   mo.Option[string]
	body   []byte
}

// sent is the answer as it is sent, or AnswerNotWritten when its member cannot be encoded.
func sent(answer Answer) sentAnswer {
	return mo.Fold(answer.body, func(body []byte) sentAnswer {
		return sentAnswer{status: answer.status, tag: answer.tag, code: answer.code, body: body}
	}, func(cause error) sentAnswer {
		failed := NocontractAnswerSharedFailure(AnswerNotWritten{Answer: answer.tag, Reason: cause.Error()})
		// AnswerNotWritten holds only text, which is always encoded, so an empty body is never sent.
		body := mo.Fold(failed.body, func(body []byte) []byte { return body }, func(error) []byte { return []byte{} })
		return sentAnswer{status: failed.status, tag: failed.tag, code: failed.code, body: body}
	})
}

// write writes the answer's status and its JSON body.
func (answer sentAnswer) write(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(answer.status)
	// The status is already sent, so a body the client stopped reading has no one left to tell.
	_, _ = writer.Write(answer.body)
}
