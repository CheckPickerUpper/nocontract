package running

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/outcome"
)

// NativeSourceFormattingFailed is generated source the language formatter could not read.
//
//ergo:code NC-1151
//ergo:message The generated source {Path} could not be formatted.
type NativeSourceFormattingFailed struct {
	Path        string
	Source      []byte
	Arguments   []string
	Diagnostics string
	Duration    time.Duration
	Cause       error
}

func (failed NativeSourceFormattingFailed) Error() string {
	return fmt.Sprintf("formatting generated %s: %v\n%s", failed.Path, failed.Cause, failed.Diagnostics)
}

func (failed NativeSourceFormattingFailed) Unwrap() error { return failed.Cause }

func nativeSourceOutput(context context.Context, file native.File) renderOutput {
	if filepath.Ext(file.Path) != ".gleam" {
		return renderedOutput(file.Contents)
	}
	return func() outcome.Result[[]byte] {
		arguments := []string{"format", "--stdin"}
		command := exec.CommandContext(context, "gleam", arguments...)
		command.Stdin = bytes.NewReader(file.Contents)
		diagnostics := &bytes.Buffer{}
		command.Stderr = diagnostics
		started := time.Now()
		return outcome.Fold(outcome.TupleToResult(command.Output()), outcome.Ok[[]byte], func(cause error) outcome.Result[[]byte] {
			return outcome.Err[[]byte](NativeSourceFormattingFailed{Path: file.Path, Source: file.Contents, Arguments: arguments, Diagnostics: diagnostics.String(), Duration: time.Since(started), Cause: cause})
		})
	}
}
