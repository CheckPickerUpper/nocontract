package nocontract

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/sync/errgroup"
)

// RequestDeadlineOutside is a duration that cannot configure a request deadline.
//
//ergo:code NC-1171
//ergo:message The request deadline {TimeoutMilliseconds} milliseconds is outside {Least} to {Most}.
type RequestDeadlineOutside struct {
	TimeoutMilliseconds uint32
	Least               uint32
	Most                uint32
}

// Error identifies the rejected duration and the accepted bounds.
func (failure RequestDeadlineOutside) Error() string {
	return fmt.Sprintf("request deadline %d milliseconds is outside %d to %d", failure.TimeoutMilliseconds, failure.Least, failure.Most)
}

// WithRequestDeadline chooses a positive whole number of milliseconds for each declared route.
// Without this option, requests keep the lifetime supplied by their caller.
func WithRequestDeadline(options ListenOptions, milliseconds uint32) outcome.Result[ListenOptions] {
	if milliseconds == 0 {
		return outcome.Err[ListenOptions](RequestDeadlineOutside{TimeoutMilliseconds: milliseconds, Least: 1, Most: 4_294_967_295})
	}
	return outcome.Ok(ListenOptions{address: options.address, requestLogger: options.requestLogger, deadline: outcome.Some(milliseconds), bodyLimit: options.bodyLimit})
}

// requestCompletion is the terminal result of one request's lifetime.
//
//sumtype:decl
type requestCompletion interface{ isRequestCompletion() }

//ergo:not-an-error
type requestCompleted struct{ answer Answered }

//ergo:not-an-error
type requestExpired struct {
	route        string
	milliseconds uint32
}

//ergo:not-an-error
type requestCancelled struct{ cause error }

func (requestCompleted) isRequestCompletion() {}
func (*requestExpired) isRequestCompletion()  {}
func (requestCancelled) isRequestCompletion() {}

// Error is the deadline cause visible through context.Cause.
func (expired *requestExpired) Error() string {
	return fmt.Sprintf("the request to %s exceeded its %d millisecond deadline", expired.route, expired.milliseconds)
}

// The worker owns a value and a buffered send. Only the listener owns the response writer.
// Shutdown joins workers after net/http has stopped accepting and finished its request calls.
func answerBeforeDeadline(parentContext context.Context, workers *errgroup.Group, route string, milliseconds uint32, compute func(context.Context) Answered) requestCompletion {
	expiration := &requestExpired{route: route, milliseconds: milliseconds}
	requestContext, cancel := context.WithTimeoutCause(parentContext, time.Duration(milliseconds)*time.Millisecond, expiration)
	defer cancel()
	completed := make(chan Answered, 1)
	workers.Go(func() error { completed <- compute(requestContext); return nil })
	select {
	case answer := <-completed:
		select {
		case <-requestContext.Done():
			return requestCancellation(requestContext, expiration)
		default:
			return requestCompleted{answer: answer}
		}
	case <-requestContext.Done():
		return requestCancellation(requestContext, expiration)
	}
}

func completeRequest(matched requestMatch, requestContext context.Context, deadline outcome.Option[uint32], workers *errgroup.Group, compute func(context.Context) Answered) requestCompletion {
	return outcome.FoldOption(deadline, func(milliseconds uint32) requestCompletion {
		switch match := matched.(type) {
		case endpointMatched:
			return answerBeforeDeadline(requestContext, workers, declaredPath(match.candidate.endpoint.Path), milliseconds, compute)
		case pathMatched, nothingMatched, pathUnreadable, nil:
		}
		return requestCompleted{answer: compute(requestContext)}
	}, func() requestCompletion { return requestCompleted{answer: compute(requestContext)} })
}

func requestCancellation(requestContext context.Context, expiration *requestExpired) requestCompletion {
	cause := context.Cause(requestContext)
	if errors.Is(expiration, cause) {
		return expiration
	}
	return requestCancelled{cause: cause}
}
