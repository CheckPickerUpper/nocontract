// Package scratchproject writes small NoContract projects into a test's temporary folder, each a
// Go module that reads a checkout of nocontract, for tests that run generate and check on them.
package scratchproject

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/mod/modfile"

	"github.com/CheckPickerUpper/nocontract/internal/running"
)

// LedgerRoutes is a package with one route, whose outcome has one member.
const LedgerRoutes = `// Package ledger reads balances.
package ledger

import "context"

// ReadInput names the shopper.
type ReadInput struct {
	ShopperID int ` + "`path:\"shopper_id\"`" + `
}

// ReadOutcome is what reading a balance ended as.
//
//sumtype:decl
type ReadOutcome interface{ isReadOutcome() }

// BalanceRead is the shopper's balance.
//
//ergo:not-an-error
//nocontract:status 200
type BalanceRead struct {
	Cents int ` + "`json:\"cents\"`" + `
}

func (BalanceRead) isReadOutcome() {}

// ReadBalance reads one shopper's balance.
//
//nocontract:route GET /shoppers/{shopper_id}/balance
func ReadBalance(ctx context.Context, input ReadInput) ReadOutcome {
	return BalanceRead{Cents: input.ShopperID}
}
`

// StopOn stops the test with the step's name and cause when the step failed.
func StopOn(t testing.TB, step string, failure error) {
	t.Helper()
	outcome.ForEachOption(outcome.EmptyableToOption(failure), func(cause error) { t.Fatalf("%s: %v", step, cause) })
}

// BytesOrStop is the content a step produced, or stops the test with the step's name and cause.
func BytesOrStop(t testing.TB, step string, produced outcome.Result[[]byte]) []byte {
	t.Helper()
	return outcome.Fold(produced, func(content []byte) []byte { return content }, func(cause error) []byte {
		t.Fatalf("%s: %v", step, cause)
		return []byte{}
	})
}

// PathOrStop is the path a step produced, or stops the test with the step's name and cause.
func PathOrStop(t testing.TB, step string, produced outcome.Result[string]) string {
	t.Helper()
	return outcome.Fold(produced, func(path string) string { return path }, func(cause error) string {
		t.Fatalf("%s: %v", step, cause)
		return ""
	})
}

// WriteFile writes content to the file at the slash-separated path under directory.
func WriteFile(t testing.TB, directory string, path string, content []byte) {
	t.Helper()
	file := filepath.Join(directory, filepath.FromSlash(path))
	StopOn(t, "making the folder of "+file, os.MkdirAll(filepath.Dir(file), 0o755))
	StopOn(t, "writing "+file, os.WriteFile(file, content, 0o644))
}

// modFile is the shop's go.mod with nocontract read from checkout.
func modFile(shopModFile []byte, checkout string) outcome.Result[[]byte] {
	return outcome.Fold(outcome.TupleToResult(modfile.Parse("go.mod", shopModFile, nil)), func(module *modfile.File) outcome.Result[[]byte] {
		return outcome.FoldOption(outcome.EmptyableToOption(module.AddReplace("github.com/CheckPickerUpper/nocontract", "", checkout, "")), outcome.Err[[]byte],
			func() outcome.Result[[]byte] { return outcome.TupleToResult(module.Format()) })
	}, outcome.Err[[]byte])
}

// ledgerFile is one file of the ledger project: its slash-separated path and what it holds.
type ledgerFile struct {
	path    string
	content []byte
}

// ledgerFiles is every file of the ledger project, in a scratch module that reads the nocontract
// checkout at checkout, built as the example shop's module is, or why one could not be made.
func ledgerFiles(checkout string) outcome.Result[[]ledgerFile] {
	return outcome.Fold(outcome.TupleToResult(filepath.Abs(checkout)), func(root string) outcome.Result[[]ledgerFile] {
		shop := filepath.Join(root, "testdata", "go", "shop")
		return outcome.Fold(outcome.TupleToResult(os.ReadFile(filepath.Join(shop, "go.mod"))), func(shopModFile []byte) outcome.Result[[]ledgerFile] {
			return outcome.Fold(modFile(shopModFile, root), func(scratchModFile []byte) outcome.Result[[]ledgerFile] {
				return outcome.Fold(outcome.TupleToResult(os.ReadFile(filepath.Join(shop, "go.sum"))), func(sums []byte) outcome.Result[[]ledgerFile] {
					return outcome.Ok([]ledgerFile{
						{path: "go.mod", content: scratchModFile},
						{path: "go.sum", content: sums},
						{path: "nocontract.json", content: []byte(`{"language": "go", "packages": ["./..."], "typescript": "web/ledger_api.generated.ts", "api": "LedgerApi", ` +
							`"openapi": {"file": "ledger_api.openapi.json", "version": "1.0.0"}, "server": "server"}`)},
						{path: "ledger/ledger_routes.go", content: []byte(LedgerRoutes)},
					})
				}, outcome.Err[[]ledgerFile])
			}, outcome.Err[[]ledgerFile])
		}, outcome.Err[[]ledgerFile])
	}, outcome.Err[[]ledgerFile])
}

// Ledger is a project holding the ledger package, in a scratch module that reads the nocontract
// checkout at the relative path checkout, built as the example shop's module is.
func Ledger(t testing.TB, checkout string) string {
	t.Helper()
	directory := t.TempDir()
	files := outcome.Fold(ledgerFiles(checkout), func(files []ledgerFile) []ledgerFile { return files }, func(cause error) []ledgerFile {
		t.Fatalf("making the ledger project: %v", cause)
		return []ledgerFile{}
	})
	for _, file := range files {
		WriteFile(t, directory, file.path, file.content)
	}
	return directory
}

// generatedLedgerVariable names the environment variable holding the folder of the generated
// ledger project a test binary builds once.
const generatedLedgerVariable = "NOCONTRACT_GENERATED_LEDGER"

// generatedLedgerUnbuilt is a test binary that could not build its generated ledger project, and
// the step that failed.
//
//ergo:not-an-error
type generatedLedgerUnbuilt struct {
	step  string
	cause error
}

// unbuiltAt is the step that failed with cause, absent when it did not fail.
func unbuiltAt(step string, failure error, next func() outcome.Option[generatedLedgerUnbuilt]) outcome.Option[generatedLedgerUnbuilt] {
	return outcome.FoldOption(outcome.EmptyableToOption(failure), func(cause error) outcome.Option[generatedLedgerUnbuilt] {
		return outcome.Some(generatedLedgerUnbuilt{step: step, cause: cause})
	}, next)
}

// writeLedgerFiles writes each file under directory, or is the first step that failed.
func writeLedgerFiles(directory string, files []ledgerFile) outcome.Option[generatedLedgerUnbuilt] {
	if len(files) == 0 {
		return outcome.None[generatedLedgerUnbuilt]()
	}
	path := filepath.Join(directory, filepath.FromSlash(files[0].path))
	return unbuiltAt("making the folder of "+path, os.MkdirAll(filepath.Dir(path), 0o755), func() outcome.Option[generatedLedgerUnbuilt] {
		return unbuiltAt("writing "+path, os.WriteFile(path, files[0].content, 0o644), func() outcome.Option[generatedLedgerUnbuilt] {
			return writeLedgerFiles(directory, files[1:])
		})
	})
}

// buildGeneratedLedger writes the ledger project into directory and runs generate on it, or is the
// step that failed.
func buildGeneratedLedger(ctx context.Context, directory, checkout string) outcome.Option[generatedLedgerUnbuilt] {
	return outcome.Fold(ledgerFiles(checkout), func(files []ledgerFile) outcome.Option[generatedLedgerUnbuilt] {
		return outcome.FoldOption(writeLedgerFiles(directory, files), outcome.Some[generatedLedgerUnbuilt], func() outcome.Option[generatedLedgerUnbuilt] {
			generated := running.Run(running.Request{Arguments: []string{"generate", "-project", filepath.Join(directory, "nocontract.json")}, Context: ctx})
			if _, isWritten := generated.(running.FilesWritten); !isWritten {
				return outcome.Some(generatedLedgerUnbuilt{step: "generating the ledger project", cause: generateRefused{lines: generated.Lines()}})
			}
			return outcome.None[generatedLedgerUnbuilt]()
		})
	}, func(cause error) outcome.Option[generatedLedgerUnbuilt] {
		return outcome.Some(generatedLedgerUnbuilt{step: "making the ledger project", cause: cause})
	})
}

// generateRefused is a generate that wrote nothing, and what it answered.
type generateRefused struct{ lines []string }

// Error is what generate answered, one line after another.
func (refused generateRefused) Error() string { return strings.Join(refused.lines, "\n") }

// RunWithGeneratedLedger runs a test binary's tests with the ledger project built and generated
// once, for GeneratedLedger to copy, and is the binary's exit code. A test binary's TestMain calls
// it with the relative path of the nocontract checkout.
func RunWithGeneratedLedger(ctx context.Context, tests *testing.M, checkout string) int {
	return outcome.Fold(outcome.TupleToResult(os.MkdirTemp("", "generated-ledger")), func(directory string) int {
		defer func() { _ = os.RemoveAll(directory) }()
		return outcome.FoldOption(buildGeneratedLedger(ctx, directory, checkout), func(unbuilt generatedLedgerUnbuilt) int {
			fmt.Fprintf(os.Stderr, "%s: %v\n", unbuilt.step, unbuilt.cause)
			return 1
		}, func() int {
			return outcome.FoldOption(outcome.EmptyableToOption(os.Setenv(generatedLedgerVariable, directory)), func(cause error) int {
				fmt.Fprintf(os.Stderr, "naming the generated ledger project: %v\n", cause)
				return 1
			}, tests.Run)
		})
	}, func(cause error) int {
		fmt.Fprintf(os.Stderr, "making the folder of the generated ledger project: %v\n", cause)
		return 1
	})
}

// GeneratedLedger is a copy, the test's own, of the ledger project after generate, which the test
// binary built once with RunWithGeneratedLedger.
func GeneratedLedger(t testing.TB) string {
	t.Helper()
	built, isBuilt := os.LookupEnv(generatedLedgerVariable)
	if !isBuilt {
		t.Fatalf("the generated ledger project was not built; run the tests with RunWithGeneratedLedger")
	}
	directory := filepath.Join(t.TempDir(), "ledger")
	StopOn(t, "copying the generated ledger project", os.CopyFS(directory, os.DirFS(built)))
	return directory
}
