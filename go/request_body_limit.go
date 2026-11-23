package nocontract

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/CheckPickerUpper/outcome"
)

// RequestBodyLimit bounds the bytes read before a request body is decoded. A zero limit permits
// only an empty body.
type RequestBodyLimit struct {
	bytes int64
}

// InvalidRequestBodyLimit is a configured byte limit below zero.
//
//ergo:code NC-1167
//ergo:message The request body limit {Bytes} is below zero.
type InvalidRequestBodyLimit struct {
	Bytes int64
}

// Error describes the configured limit that cannot bound a request body.
func (failure InvalidRequestBodyLimit) Error() string {
	return fmt.Sprintf("the request body limit %d is below zero", failure.Bytes)
}

// ParseRequestBodyLimit accepts a nonnegative byte limit for the native body reader.
func ParseRequestBodyLimit(bytes int64) outcome.Result[RequestBodyLimit] {
	if bytes < 0 {
		return outcome.Err[RequestBodyLimit](InvalidRequestBodyLimit{Bytes: bytes})
	}
	return outcome.Ok(RequestBodyLimit{bytes: bytes})
}

// DefaultRequestBodyLimit is the eight million byte limit used when an application does not
// choose another limit.
func DefaultRequestBodyLimit() RequestBodyLimit {
	return RequestBodyLimit{bytes: 8_000_000}
}

// RequestBodyReceiveFailed keeps the bytes received before the underlying reader stopped and
// the error that stopped it.
//
//ergo:not-an-error
type RequestBodyReceiveFailed struct {
	Limit   int64
	Content []byte
	Cause   error
}

func (RequestBodyReceiveFailed) isReading() {}

// readRequestBody checks declared length before reading and bounds native reads to the first
// excess byte. Only complete bounded bytes reach the decoder. The server owns closing the body.
func readRequestBody[V any](request *http.Request, limit RequestBodyLimit, decode func([]byte) Reading[V]) Reading[V] {
	if request.ContentLength > limit.bytes {
		return RequestBodyTooLarge{LimitBytes: uint64(limit.bytes), SizeBytes: uint64(request.ContentLength), SizeSource: RequestBodyTooLargeSizeSourceContentLength}
	}
	bounded := http.MaxBytesReader(nil, request.Body, limit.bytes)
	content, readError := io.ReadAll(bounded)
	return outcome.Fold(outcome.TupleToResult(content, readError), decode, func(cause error) Reading[V] {
		if _, exceeded := errors.AsType[*http.MaxBytesError](cause); exceeded {
			return RequestBodyTooLarge{LimitBytes: uint64(limit.bytes), SizeBytes: uint64(limit.bytes) + 1, SizeSource: RequestBodyTooLargeSizeSourceStream}
		}
		return RequestBodyReceiveFailed{Limit: limit.bytes, Content: content, Cause: cause}
	})
}

func answerBodyTooLarge(cause RequestBodyTooLarge) Answer {
	answer := NocontractAnswerSharedFailure(cause)
	return Answer{status: answer.status, tag: answer.tag, code: answer.code, body: answer.body, bodyRefusal: outcome.Some(cause)}
}

// WithRequestBodyLimit selects the validated byte cap for JSON request bodies.
func WithRequestBodyLimit(options ListenOptions, limit RequestBodyLimit) ListenOptions {
	return ListenOptions{address: options.address, requestLogger: options.requestLogger, bodyLimit: limit, deadline: options.deadline}
}
