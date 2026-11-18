package projectfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/CheckPickerUpper/outcome"
)

// readValue is one JSON value of the project file: the byte offset it starts at, its JSON text,
// and its shape.
type readValue struct {
	start   int
	written string
	shape   jsonShape
}

// jsonShape is what a JSON value is.
//
//sumtype:decl
type jsonShape interface{ isJSONShape() }

// jsonObject is an object's fields, in the order they are written, repeats included.
//
//ergo:not-an-error
type jsonObject struct{ fields []jsonField }

// jsonArray is an array's items.
//
//ergo:not-an-error
type jsonArray struct{ items []readValue }

// jsonText is a string, unescaped.
//
//ergo:not-an-error
type jsonText struct{ text string }

// jsonScalar is a number, true, false or null.
//
//ergo:not-an-error
type jsonScalar struct{}

func (jsonObject) isJSONShape() {}
func (jsonArray) isJSONShape()  {}
func (jsonText) isJSONShape()   {}
func (jsonScalar) isJSONShape() {}

// jsonField is one field of an object: its key, the offset the key starts at, and its value.
type jsonField struct {
	key      string
	keyStart int
	value    readValue
}

// jsonSyntaxFailure is content that is not one JSON value: the offset reading stopped at, and why.
type jsonSyntaxFailure struct {
	offset int
	reason string
}

// Error is why the content is not JSON.
func (failure jsonSyntaxFailure) Error() string { return failure.reason }

// jsonReader reads one JSON value from content, keeping where each value starts.
type jsonReader struct {
	content []byte
	decoder *json.Decoder
}

// foldJSON is read applied to the one JSON value content holds, or failed applied to where and
// why content is not one.
func foldJSON[R any](content []byte, read func(readValue) R, failed func(jsonSyntaxFailure) R) R {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	reader := jsonReader{content: content, decoder: decoder}
	return outcome.Fold(reader.value(), func(root readValue) R {
		if decoder.More() {
			return failed(jsonSyntaxFailure{offset: reader.nextStart(), reason: "more than one value is written"})
		}
		return read(root)
	}, func(cause error) R {
		// Every failure the reader makes is a jsonSyntaxFailure; any other stopped where the
		// decoder stands.
		if failure, isFailure := errors.AsType[jsonSyntaxFailure](cause); isFailure {
			return failed(failure)
		}
		return failed(jsonSyntaxFailure{offset: int(decoder.InputOffset()), reason: cause.Error()})
	})
}

// nextStart is the offset the next token starts at, past the separators before it.
func (reader jsonReader) nextStart() int {
	offset := int(reader.decoder.InputOffset())
	for offset < len(reader.content) && strings.ContainsRune(" \t\r\n:,", rune(reader.content[offset])) {
		offset++
	}
	return offset
}

// token is the next token, or where and why the content stops being JSON.
func (reader jsonReader) token() outcome.Result[json.Token] {
	return outcome.Fold(outcome.TupleToResult(reader.decoder.Token()), outcome.Ok[json.Token], func(cause error) outcome.Result[json.Token] {
		offset := int(reader.decoder.InputOffset())
		if syntax, isSyntax := errors.AsType[*json.SyntaxError](cause); isSyntax {
			offset = int(syntax.Offset)
		}
		if errors.Is(cause, io.EOF) {
			return outcome.Err[json.Token](jsonSyntaxFailure{offset: offset, reason: "the content ends before a value"})
		}
		return outcome.Err[json.Token](jsonSyntaxFailure{offset: offset, reason: cause.Error()})
	})
}

// finished is the value that started at start and has just been read.
func (reader jsonReader) finished(start int, shape jsonShape) readValue {
	return readValue{start: start, written: string(reader.content[start:reader.decoder.InputOffset()]), shape: shape}
}

// value reads the next value.
func (reader jsonReader) value() outcome.Result[readValue] {
	start := reader.nextStart()
	return outcome.Fold(reader.token(), func(token json.Token) outcome.Result[readValue] {
		switch read := token.(type) {
		case json.Delim:
			if read == '{' {
				return outcome.Fold(reader.fields([]jsonField{}), func(fields []jsonField) outcome.Result[readValue] {
					return reader.closed(start, jsonObject{fields: fields})
				}, outcome.Err[readValue])
			}
			return outcome.Fold(reader.items([]readValue{}), func(items []readValue) outcome.Result[readValue] {
				return reader.closed(start, jsonArray{items: items})
			}, outcome.Err[readValue])
		case string:
			return outcome.Ok(reader.finished(start, jsonText{text: read}))
		case nil:
			// A JSON null is read as no token.
		}
		return outcome.Ok(reader.finished(start, jsonScalar{}))
	}, outcome.Err[readValue])
}

// closed reads the delimiter closing the object or array that started at start.
func (reader jsonReader) closed(start int, shape jsonShape) outcome.Result[readValue] {
	return outcome.Fold(reader.token(), func(json.Token) outcome.Result[readValue] {
		return outcome.Ok(reader.finished(start, shape))
	}, outcome.Err[readValue])
}

// fields reads the rest of an object's fields after read.
func (reader jsonReader) fields(read []jsonField) outcome.Result[[]jsonField] {
	if !reader.decoder.More() {
		return outcome.Ok(read)
	}
	keyStart := reader.nextStart()
	return outcome.Fold(reader.token(), func(token json.Token) outcome.Result[[]jsonField] {
		key, isKey := token.(string)
		if !isKey {
			return outcome.Err[[]jsonField](jsonSyntaxFailure{offset: keyStart, reason: "an object key is not a string"})
		}
		return outcome.Fold(reader.value(), func(value readValue) outcome.Result[[]jsonField] {
			return reader.fields(append(read, jsonField{key: key, keyStart: keyStart, value: value}))
		}, outcome.Err[[]jsonField])
	}, outcome.Err[[]jsonField])
}

// items reads the rest of an array's items after read.
func (reader jsonReader) items(read []readValue) outcome.Result[[]readValue] {
	if !reader.decoder.More() {
		return outcome.Ok(read)
	}
	return outcome.Fold(reader.value(), func(value readValue) outcome.Result[[]readValue] {
		return reader.items(append(read, value))
	}, outcome.Err[[]readValue])
}
