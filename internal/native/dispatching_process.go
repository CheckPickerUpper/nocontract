package native

import (
	"bufio"
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/sync/errgroup"
)

// Dispatcher calls the compiled project and owns its stdin and stdout.
type Dispatcher struct {
	call  func(context.Context, invocation) outcome.Result[reply]
	close func() error
}

// Close allows the native process to finish reading its input before waiting for it to exit.
func (dispatcher Dispatcher) Close() error { return dispatcher.close() }

// Start waits for the native dispatcher to report readiness.
func Start(lifetime context.Context, command Command) outcome.Result[Dispatcher] {
	process := exec.CommandContext(lifetime, command.Executable, command.Arguments...)
	process.Dir = command.Directory
	process.WaitDelay = 2 * time.Second
	stderr := &processLog{mutex: &sync.Mutex{}, capture: outcome.Some(&bytes.Buffer{})}
	process.Stderr = io.MultiWriter(os.Stderr, stderr)
	fail := func(step string, cause error) outcome.Result[Dispatcher] {
		return outcome.Err[Dispatcher](DispatcherStartFailed{Command: command, Step: step, Diagnostics: stderr.text(), Cause: cause})
	}
	return outcome.Fold(outcome.TupleToResult(process.StdinPipe()), func(input io.WriteCloser) outcome.Result[Dispatcher] {
		return outcome.Fold(outcome.TupleToResult(process.StdoutPipe()), func(output io.ReadCloser) outcome.Result[Dispatcher] {
			return outcome.Fold(outcome.TupleToResult(struct{}{}, process.Start()), func(struct{}) outcome.Result[Dispatcher] {
				return outcome.Fold(connectedDispatcher(lifetime, process, command, input, output, fail), func(dispatcher Dispatcher) outcome.Result[Dispatcher] {
					stderr.release()
					return outcome.Ok(dispatcher)
				}, outcome.Err[Dispatcher])
			}, func(cause error) outcome.Result[Dispatcher] { return fail("start", cause) })
		}, func(cause error) outcome.Result[Dispatcher] {
			closeError := input.Close()
			return fail("open stdout", errors.Join(cause, closeError))
		})
	}, func(cause error) outcome.Result[Dispatcher] { return fail("open stdin", cause) })
}

type processLog struct {
	mutex   *sync.Mutex
	capture outcome.Option[*bytes.Buffer]
}

func (log *processLog) Write(contents []byte) (int, error) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	writer := outcome.FoldOption(log.capture, func(output *bytes.Buffer) io.Writer { return output }, func() io.Writer { return io.Discard })
	return writer.Write(contents)
}

func (log *processLog) text() string {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	return outcome.FoldOption(log.capture, (*bytes.Buffer).String, func() string { return "" })
}

func (log *processLog) release() {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.capture = outcome.None[*bytes.Buffer]()
}

func connectedDispatcher(lifetime context.Context, process *exec.Cmd, command Command, input io.WriteCloser, output io.ReadCloser,
	fail func(string, error) outcome.Result[Dispatcher],
) outcome.Result[Dispatcher] {
	closeInput, closeOutput := sync.OnceValue(input.Close), sync.OnceValue(output.Close)
	interrupted := context.AfterFunc(lifetime, func() {
		outcome.ForEachOption(outcome.EmptyableToOption(errors.Join(closeInput(), closeOutput())), func(cause error) {
			fmt.Fprintln(os.Stderr, cause)
		})
	})
	closeProcess := sync.OnceValue(func() error {
		defer interrupted()
		return errors.Join(closeInput(), process.Wait())
	})
	lines := bufio.NewReader(output)
	return outcome.Fold(outcome.TupleToResult(lines.ReadString('\n')), func(line string) outcome.Result[Dispatcher] {
		if strings.TrimRight(line, "\r\n") != "nocontract-ready" {
			return fail("read readiness", errors.Join(dispatcherNotReady{Line: line}, process.Cancel(), closeProcess()))
		}
		loop := startInvocations(lifetime, command, input, lines)
		return outcome.Ok(Dispatcher{call: loop.call, close: sync.OnceValue(func() error {
			loop.stop()
			return errors.Join(closeProcess(), loop.wait())
		})})
	}, func(cause error) outcome.Result[Dispatcher] {
		return fail("read readiness", errors.Join(cause, lifetime.Err(), process.Cancel(), closeProcess()))
	})
}

type invocationLoop struct {
	call func(context.Context, invocation) outcome.Result[reply]
	stop context.CancelFunc
	wait func() error
}

type waitingInvocation struct {
	request invocation
	answer  chan outcome.Result[reply]
}

func invocationFailed(command Command, request invocation, step string, cause error) outcome.Result[reply] {
	return outcome.Err[reply](InvocationFailed{Command: command, Step: step, Operation: request.Operation, Cause: cause})
}

func startInvocations(lifetime context.Context, command Command, input io.Writer, output *bufio.Reader) invocationLoop {
	workers, stop := context.WithCancel(lifetime)
	group, running := errgroup.WithContext(workers)
	queue := make(chan waitingInvocation)
	group.Go(func() error {
		for {
			select {
			case <-running.Done():
				return nil
			case next := <-queue:
				next.answer <- exchangeInvocation(input, output, next.request, func(step string, cause error) outcome.Result[reply] {
					return invocationFailed(command, next.request, step, cause)
				})
			}
		}
	})
	call := func(caller context.Context, request invocation) outcome.Result[reply] {
		finished := make(chan outcome.Result[reply], 1)
		select {
		case queue <- waitingInvocation{request: request, answer: finished}:
		case <-caller.Done():
			return invocationFailed(command, request, "wait for dispatcher", caller.Err())
		case <-running.Done():
			return invocationFailed(command, request, "wait for dispatcher", running.Err())
		}
		select {
		case received := <-finished:
			return received
		case <-caller.Done():
			return invocationFailed(command, request, "wait for reply", caller.Err())
		case <-running.Done():
			return invocationFailed(command, request, "wait for reply", running.Err())
		}
	}
	return invocationLoop{call: call, stop: stop, wait: group.Wait}
}

func exchangeInvocation(input io.Writer, output *bufio.Reader, request invocation,
	fail func(string, error) outcome.Result[reply],
) outcome.Result[reply] {
	return outcome.Fold(outcome.TupleToResult(jsonv2.Marshal(request)), func(encoded []byte) outcome.Result[reply] {
		return outcome.Fold(outcome.TupleToResult(input.Write(append(encoded, '\n'))), func(int) outcome.Result[reply] {
			return outcome.Fold(outcome.TupleToResult(output.ReadBytes('\n')), func(line []byte) outcome.Result[reply] {
				return decodeReply(request.Operation, line)
			}, func(cause error) outcome.Result[reply] { return fail("read reply", cause) })
		}, func(cause error) outcome.Result[reply] { return fail("write invocation", cause) })
	}, func(cause error) outcome.Result[reply] { return fail("encode invocation", cause) })
}
