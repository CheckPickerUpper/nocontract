package nocontract

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/messages"
)

// Reading is what reading one part of a request as a V ended as.
//
//sumtype:decl
type Reading[V any] interface {
	isReading()
}

// Read is a part of the request read as the V its input field holds.
//
//ergo:not-an-error
type Read[V any] struct {
	Value V
}

func (Read[V]) isReading()            {}
func (RequestRefused) isReading()     {}
func (RequestRefused) isTextReading() {}
func (textPresent) isTextReading()    {}

// Then is next's answer for the value reading read, or the refusal when reading failed. Generated
// code chains one Then for each part of the request a route reads.
func Then[V any](reading Reading[V], next func(V) Answer) Answer {
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
func parsed[V any](text Text, parse func(string) mo.Result[V]) Reading[V] {
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

// Textual is the part's text as it is, as the text type T its field holds.
func Textual[T ~string](text Text) Reading[T] {
	return parsed(text, func(read string) mo.Result[T] { return mo.Ok(T(read)) })
}

// Signed is the part's text as the whole number type I its field holds, refused when it does not
// fit I.
func Signed[I ~int | ~int8 | ~int16 | ~int32 | ~int64](text Text) Reading[I] {
	return parsed(text, func(read string) mo.Result[I] {
		return mo.Fold(mo.TupleToResult(strconv.ParseInt(read, 10, 64)), func(whole int64) mo.Result[I] {
			if fitted := I(whole); int64(fitted) == whole {
				return mo.Ok(fitted)
			}
			// The refusal strconv gives a number too large for the size it is asked to parse.
			return mo.Err[I](&strconv.NumError{Func: "ParseInt", Num: read, Err: strconv.ErrRange})
		}, mo.Err[I])
	})
}

// Unsigned is the part's text as the whole number type U at or above zero its field holds, refused
// when it does not fit U.
func Unsigned[U ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr](text Text) Reading[U] {
	return parsed(text, func(read string) mo.Result[U] {
		return mo.Fold(mo.TupleToResult(strconv.ParseUint(read, 10, 64)), func(whole uint64) mo.Result[U] {
			if fitted := U(whole); uint64(fitted) == whole {
				return mo.Ok(fitted)
			}
			// The refusal strconv gives a number too large for the size it is asked to parse.
			return mo.Err[U](&strconv.NumError{Func: "ParseUint", Num: read, Err: strconv.ErrRange})
		}, mo.Err[U])
	})
}

// Truth is the part's text as the boolean type B its field holds.
func Truth[B ~bool](text Text) Reading[B] {
	return parsed(text, func(read string) mo.Result[B] {
		return mo.Fold(mo.TupleToResult(strconv.ParseBool(read)), func(truth bool) mo.Result[B] { return mo.Ok(B(truth)) }, mo.Err[B])
	})
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

// Literal is the part's text as the string type L its field holds, when it is one of values.
func Literal[L ~string](text Text, values ...L) Reading[L] {
	return parsed(text, func(read string) mo.Result[L] {
		if !slices.Contains(values, L(read)) {
			accepted := []string{}
			for _, value := range values {
				accepted = append(accepted, string(value))
			}
			return mo.Err[L](LiteralUnknown{Text: read, Values: accepted})
		}
		return mo.Ok(L(read))
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
