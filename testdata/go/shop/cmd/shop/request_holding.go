package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/CheckPickerUpper/outcome"
)

type requestReleased struct{}

type requestNotReleased struct {
	Address string
	Cause   error
}

func (failure requestNotReleased) Error() string {
	return fmt.Sprintf("holding the shop request through %s: %v", failure.Address, failure.Cause)
}

func (failure requestNotReleased) Unwrap() error { return failure.Cause }

// The opt-in control connection proves application entry and echoes the release byte.
func holdShopRequest(requestContext context.Context, address string) outcome.Result[requestReleased] {
	holdContext, cancel := context.WithTimeout(requestContext, 5*time.Second)
	defer cancel()
	failed := func(cause error) outcome.Result[requestReleased] {
		return outcome.Err[requestReleased](requestNotReleased{Address: address, Cause: cause})
	}
	dialer := &net.Dialer{}
	return outcome.Fold(outcome.TupleToResult(dialer.DialContext(holdContext, "tcp", address)), func(connection net.Conn) outcome.Result[requestReleased] {
		deadline, _ := holdContext.Deadline()
		released := outcome.Fold(outcome.TupleToResult(struct{}{}, connection.SetDeadline(deadline)), func(struct{}) outcome.Result[requestReleased] {
			release := [1]byte{}
			return outcome.Fold(outcome.TupleToResult(io.ReadFull(connection, release[:])), func(int) outcome.Result[requestReleased] {
				return outcome.FoldOption(outcome.EmptyableToOption(holdContext.Err()), failed, func() outcome.Result[requestReleased] {
					return outcome.Fold(outcome.TupleToResult(io.CopyN(connection, bytes.NewReader(release[:]), 1)), func(int64) outcome.Result[requestReleased] {
						return outcome.Ok(requestReleased{})
					}, failed)
				})
			}, failed)
		}, failed)
		closeFailure := connection.Close()
		return outcome.Fold(released, func(released requestReleased) outcome.Result[requestReleased] {
			return outcome.FoldOption(outcome.EmptyableToOption(closeFailure), failed, func() outcome.Result[requestReleased] { return outcome.Ok(released) })
		}, func(cause error) outcome.Result[requestReleased] {
			return outcome.Err[requestReleased](errors.Join(cause, closeFailure))
		})
	}, failed)
}
