package nocontract

import (
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

func (Read[V]) isReading()             {}
func (RequestRefused) isReading()      {}
func (RequestBodyTooLarge) isReading() {}
func (RequestRefused) isTextReading()  {}
func (textPresent) isTextReading()     {}

// Then is next's answer for the value reading read, or the refusal when reading failed. Generated
// code chains one Then for each part of the request a route reads.
func Then[V any](reading Reading[V], next func(V) Answer) Answer {
	switch read := reading.(type) {
	case Read[V]:
		return next(read.Value)
	case RequestRefused:
		return NocontractAnswerSharedFailure(read)
	case RequestBodyTooLarge:
		return answerBodyTooLarge(read)
	case RequestBodyReceiveFailed:
		return NocontractAnswerSharedFailure(RequestRefused{Part: RequestRefusedPartBody, Name: bodyRoot, Reason: read.Cause.Error()})
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

// signedWhole is every signed whole number type a field may hold.
type signedWhole interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

// unsignedWhole is every whole number type at or above zero a field may hold.
type unsignedWhole interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// fittedSigned is whole as the type I, when I holds it.
func fittedSigned[I signedWhole](whole int64) mo.Option[I] {
	if fitted := I(whole); int64(fitted) == whole {
		return mo.Some(fitted)
	}
	return mo.None[I]()
}

// fittedUnsigned is whole as the type U, when U holds it.
func fittedUnsigned[U unsignedWhole](whole uint64) mo.Option[U] {
	if fitted := U(whole); uint64(fitted) == whole {
		return mo.Some(fitted)
	}
	return mo.None[U]()
}

// DecimalTextNotCanonical is a part's text where a whole number is read that is not canonical
// decimal text.
//
//ergo:code NC-1136
//ergo:message "{Text}" is not a whole number written as canonical decimal text: an optional -, then 0 or a digit from 1 to 9 followed by any digits, and never -0.
type DecimalTextNotCanonical struct {
	Text string
}

// Error is the failure's message in the source language.
func (cause DecimalTextNotCanonical) Error() string {
	return cause.Report(messages.Match("")).Message
}

// decimalParameter is read, a part's text, as the whole number parse reads it into, refused when it
// is not canonical decimal text, and refused as strconv's parser refuses a number too large for the
// size it is asked to parse when the number is outside its type.
func decimalParameter[V any](read string, parse func(string) mo.Result[V], parser string) mo.Result[V] {
	return mo.Fold(decimalWhole(read, parse), func(parsed mo.Result[V]) mo.Result[V] {
		return mo.Fold(parsed, mo.Ok[V], func(error) mo.Result[V] {
			return mo.Err[V](&strconv.NumError{Func: parser, Num: read, Err: strconv.ErrRange})
		})
	}, func(error) mo.Result[V] { return mo.Err[V](DecimalTextNotCanonical{Text: read}) })
}

// Signed is the part's canonical decimal text as the whole number type I its field holds, refused
// when it is not canonical decimal text or does not fit I.
func Signed[I signedWhole](text Text) Reading[I] {
	return parsed(text, func(read string) mo.Result[I] { return decimalParameter(read, parsedSigned[I], "ParseInt") })
}

// Unsigned is the part's canonical decimal text as the whole number type U at or above zero its
// field holds, refused when it is not canonical decimal text or does not fit U.
func Unsigned[U unsignedWhole](text Text) Reading[U] {
	return parsed(text, func(read string) mo.Result[U] { return decimalParameter(read, parsedUnsigned[U], "ParseUint") })
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
