package nocontract

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/messages"
)

// Value is a value read from one part of a request: text, a whole number, a boolean, or the empty
// struct a body read leaves.
type Value interface {
	~string | ~int64 | ~uint64 | ~bool | ~struct{}
}

// Reading is what reading one part of a request ended as.
//
//sumtype:decl
type Reading[V Value] interface {
	isReading()
}

// Read is a part of the request read as Value.
//
//ergo:not-an-error
type Read[V Value] struct {
	Value V
}

func (Read[V]) isReading()            {}
func (RequestRefused) isReading()     {}
func (RequestRefused) isTextReading() {}
func (textPresent) isTextReading()    {}

// Then is next's answer for the value reading read, or the refusal when reading failed. Generated
// code chains one Then for each part of the request a route reads.
func Then[V Value](reading Reading[V], next func(V) Answer) Answer {
	switch read := reading.(type) {
	case Read[V]:
		return next(read.Value)
	case RequestRefused:
		return NocontractAnswerSharedFailure(read)
	case nil:
	}
	return NoOutcome()
}

// Text is one named part of a request as the text it carries.
type Text struct {
	part    RequestRefusedPart
	name    string
	reading textReading
}

// textReading is a part's text, or the refusal of a part the request lacks.
//
//sumtype:decl
type textReading interface {
	isTextReading()
}

// textPresent is the text the part carries.
//
//ergo:not-an-error
type textPresent struct{ text string }

// Path is the path segment the route's pattern names name.
func Path(request *http.Request, name string) Text {
	return Text{part: RequestRefusedPartPath, name: name, reading: textPresent{text: request.PathValue(name)}}
}

// Query is the query parameter name, refused when the request does not carry it.
func Query(request *http.Request, name string) Text {
	query := request.URL.Query()
	if !query.Has(name) {
		return Text{part: RequestRefusedPartQuery, name: name, reading: RequestRefused{Part: RequestRefusedPartQuery, Name: name, Reason: "the query parameter is missing"}}
	}
	return Text{part: RequestRefusedPartQuery, name: name, reading: textPresent{text: query.Get(name)}}
}

// parsed reads the part's text with parse, refusing it with parse's error.
func parsed[V Value](text Text, parse func(string) mo.Result[V]) Reading[V] {
	switch read := text.reading.(type) {
	case textPresent:
		return mo.Fold(parse(read.text), func(value V) Reading[V] { return Read[V]{Value: value} }, func(cause error) Reading[V] {
			return RequestRefused{Part: text.part, Name: text.name, Reason: cause.Error()}
		})
	case RequestRefused:
		return read
	case nil:
	}
	return RequestRefused{Part: text.part, Name: text.name, Reason: "the part was not read"}
}

// Textual is the part's text as it is.
func Textual(text Text) Reading[string] {
	return parsed(text, mo.Ok[string])
}

// Signed is the part's text as a whole number that fits bits bits, 0 meaning an int's size.
func Signed(text Text, bits int) Reading[int64] {
	return parsed(text, func(read string) mo.Result[int64] { return mo.TupleToResult(strconv.ParseInt(read, 10, bits)) })
}

// Unsigned is the part's text as a whole number at or above zero that fits bits bits.
func Unsigned(text Text, bits int) Reading[uint64] {
	return parsed(text, func(read string) mo.Result[uint64] { return mo.TupleToResult(strconv.ParseUint(read, 10, bits)) })
}

// Truth is the part's text as a boolean.
func Truth(text Text) Reading[bool] {
	return parsed(text, func(read string) mo.Result[bool] { return mo.TupleToResult(strconv.ParseBool(read)) })
}

// LiteralUnknown is text that is none of the values its field accepts.
//
//ergo:code NC-1043
//ergo:message "{Text}" is not one of the values this field accepts.
type LiteralUnknown struct {
	Text   string
	Values []string
}

// Error is the failure's message in the source language.
func (unknown LiteralUnknown) Error() string {
	return unknown.Report(messages.Match("")).Message
}

// Literal is the part's text when it is one of values.
func Literal(text Text, values ...string) Reading[string] {
	return parsed(text, func(read string) mo.Result[string] {
		if !slices.Contains(values, read) {
			return mo.Err[string](LiteralUnknown{Text: read, Values: values})
		}
		return mo.Ok(read)
	})
}

// BodyNotSingle is a body holding more than one JSON value.
//
//ergo:code NC-1044
//ergo:message The body holds more than one JSON value.
type BodyNotSingle struct{}

// Error is the failure's message in the source language.
func (refused BodyNotSingle) Error() string {
	return refused.Report(messages.Match("")).Message
}

// Body reads the request's JSON body with decode, which decodes it into the input's Body field.
// The body may hold only the fields that field declares, and only one JSON value.
func Body(request *http.Request, decode func(*json.Decoder) error) Reading[struct{}] {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	refused := func(cause error) Reading[struct{}] {
		return RequestRefused{Part: RequestRefusedPartBody, Name: "Body", Reason: cause.Error()}
	}
	return mo.Fold(mo.EmptyableToOption(decode(decoder)), refused, func(error) Reading[struct{}] {
		if decoder.More() {
			return refused(BodyNotSingle{})
		}
		return Read[struct{}]{Value: struct{}{}}
	})
}
