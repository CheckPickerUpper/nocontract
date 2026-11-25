package nocontract

// nocontract_routes.go declares the failures nocontract answers on a route's behalf, from their one
// declaration in package description.
//go:generate go run ../internal/cmd/runtime-answers generate

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"net/http"
	"strconv"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"
)

// Answer is what a route writes: its member's status, its member's Go type name as the tag the
// Effect client tells members apart by, a failure's permanent code, and one JSON object holding
// the tag, the code and the member's own fields. Generated code builds it with Succeeded or Failed.
type Answer struct {
	status      int
	tag         string
	code        mo.Option[string]
	body        mo.Result[[]byte]
	bodyRefusal outcome.Option[RequestBodyTooLarge]
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

// AnswerMarshalers is the marshaler of each Go type an answer holds that encoding/json writes
// otherwise than the description declares, such as a 64-bit whole number, which it writes as decimal
// text. Generated code writes the list for every answer, empty when the answer holds no such type.
type AnswerMarshalers []*jsonv2.Marshalers

// Succeeded is a success member's answer, member's fields written after its tag, each Go type a
// marshaler of written takes written by it.
func Succeeded[M any](success Success, member M, written AnswerMarshalers) Answer {
	return Answer{
		status: success.Status, tag: success.Tag, code: mo.None[string](),
		body:        encoded(taggedSuccess[M]{Tag: success.Tag, Fields: member}, written),
		bodyRefusal: outcome.None[RequestBodyTooLarge](),
	}
}

// Failed is a failure member's answer, member's fields written after its tag and code, each Go type
// a marshaler of written takes written by it.
func Failed[M any](failure Failure, member M, written AnswerMarshalers) Answer {
	return Answer{
		status: failure.Status, tag: failure.Tag, code: mo.Some(failure.Code),
		body:        encoded(taggedFailure[M]{Tag: failure.Tag, Code: failure.Code, Fields: member}, written),
		bodyRefusal: outcome.None[RequestBodyTooLarge](),
	}
}

// encoded is value as encoding/json writes it with written, which writes an embedded member's
// fields beside the tag and code in the one object it encodes.
func encoded[V any](value V, written AnswerMarshalers) mo.Result[[]byte] {
	return mo.TupleToResult(jsonv2.Marshal(value, json.DefaultOptionsV1(), jsonv2.WithMarshalers(jsonv2.JoinMarshalers(written...))))
}

// SignedText writes the signed 64-bit type I as a JSON string of its decimal text, which a
// JavaScript client reads whole where a JSON number past 2^53 loses its last digits.
func SignedText[I ~int | ~int64]() *jsonv2.Marshalers {
	return jsonv2.MarshalToFunc(func(encoder *jsontext.Encoder, value I) error {
		return encoder.WriteToken(jsontext.String(strconv.FormatInt(int64(value), 10)))
	})
}

// UnsignedText writes the 64-bit type U at or above zero as a JSON string of its decimal text.
func UnsignedText[U ~uint | ~uint64 | ~uintptr]() *jsonv2.Marshalers {
	return jsonv2.MarshalToFunc(func(encoder *jsontext.Encoder, value U) error {
		return encoder.WriteToken(jsontext.String(strconv.FormatUint(uint64(value), 10)))
	})
}

// NullableValue writes an outcome.Option of T as null, or as the value it holds written with the
// answer's marshalers. The Option's members keep their value unexported, so encoding/json writes
// none of it without this marshaler.
func NullableValue[T any]() *jsonv2.Marshalers {
	return jsonv2.MarshalToFunc(func(encoder *jsontext.Encoder, value *outcome.Option[T]) error {
		return outcome.FoldOption(*value, func(held T) error { return jsonv2.MarshalEncode(encoder, held) }, func() error {
			return encoder.WriteToken(jsontext.Null)
		})
	})
}

// NoOutcome is the answer of a route whose handler returned no outcome.
func NoOutcome() Answer {
	return NocontractAnswerSharedFailure(AnswerNotWritten{Answer: "", Reason: "the route answered no outcome"})
}

// sentAnswer is an answer as it is sent: its status, its tag, a failure's code, and one JSON object
// holding its tag, its code and its fields.
type sentAnswer struct {
	status      int
	tag         string
	code        mo.Option[string]
	body        []byte
	bodyRefusal outcome.Option[RequestBodyTooLarge]
}

// sent is the answer as it is sent, or AnswerNotWritten when its member cannot be encoded.
func sent(answer Answer) sentAnswer {
	return mo.Fold(answer.body, func(body []byte) sentAnswer {
		return sentAnswer{status: answer.status, tag: answer.tag, code: answer.code, body: body, bodyRefusal: answer.bodyRefusal}
	}, func(cause error) sentAnswer {
		failed := NocontractAnswerSharedFailure(AnswerNotWritten{Answer: answer.tag, Reason: cause.Error()})
		// AnswerNotWritten holds only text, which is always encoded, so an empty body is never sent.
		body := mo.Fold(failed.body, func(body []byte) []byte { return body }, func(error) []byte { return []byte{} })
		return sentAnswer{status: failed.status, tag: failed.tag, code: failed.code, body: body, bodyRefusal: answer.bodyRefusal}
	})
}

// write writes the answer's status and its JSON body.
func (answer sentAnswer) write(writer http.ResponseWriter) {
	outcome.ForEachOption(answer.bodyRefusal, func(RequestBodyTooLarge) { writer.Header().Set("Connection", "close") })
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(answer.status)
	// The status is already sent, so a body the client stopped reading has no one left to tell.
	_, _ = writer.Write(answer.body)
}
