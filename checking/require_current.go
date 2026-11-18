// Package checking fails a Go test while a file nocontract generates differs from what
// `go tool nocontract generate` writes now, so `go test ./...` catches a handler changed without
// generating again.
package checking

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/internal/running"
)

// Test is what RequireCurrent needs of the test it runs in. *testing.T, *testing.B and *testing.F
// each have it.
type Test interface {
	Helper()
	Context() context.Context
	Output() io.Writer
	FailNow()
}

// RequireCurrent runs `nocontract check` on the project file at projectFile, a slash-separated path
// from the test's package folder, and fails the test with every line check reports unless every
// generated file is current.
func RequireCurrent(t Test, projectFile string) {
	t.Helper()
	checked := running.Run(running.Request{Arguments: []string{"check", "-project", filepath.FromSlash(projectFile)}, Context: t.Context()})
	if _, isCurrent := checked.(running.FilesCurrent); isCurrent {
		return
	}
	report := strings.Join(append(checked.Lines(), "Run go tool nocontract generate to write the generated files again."), "\n") + "\n"
	// The test's output is the only place a report goes, so the test fails whether or not the output
	// took it; the testing package's writer takes every write.
	outcome.Fold(outcome.TupleToResult(io.WriteString(t.Output(), report)), func(int) struct{} {
		t.FailNow()
		return struct{}{}
	}, func(error) struct{} {
		t.FailNow()
		return struct{}{}
	})
}
