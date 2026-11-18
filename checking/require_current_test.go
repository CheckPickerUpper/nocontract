package checking_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/mod/modfile"

	"github.com/CheckPickerUpper/nocontract/checking"
	"github.com/CheckPickerUpper/nocontract/internal/running"
)

// failureRecorder is a test whose FailNow records what was written to its output instead of
// stopping the test, so a test can read what RequireCurrent fails with.
type failureRecorder struct {
	testContext context.Context
	output      *strings.Builder
	failures    []string
}

// Helper marks nothing: the recorder reports no lines of its own.
func (recorder *failureRecorder) Helper() {}

// Context is the context of the test the recorder runs in.
func (recorder *failureRecorder) Context() context.Context { return recorder.testContext }

// Output is where the recorder collects what the test writes before it fails.
func (recorder *failureRecorder) Output() io.Writer { return recorder.output }

// FailNow records what was written since the last failure as one failure.
func (recorder *failureRecorder) FailNow() {
	recorder.failures = append(recorder.failures, recorder.output.String())
	recorder.output.Reset()
}

// ledgerRoutes is a package with one route, whose outcome has one member.
const ledgerRoutes = `// Package ledger reads balances.
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

// shopperMissing adds a failure to the ledger's outcome.
const shopperMissing = `package ledger

// ShopperNotFound is a shopper the ledger holds no balance for.
//
//ergo:code LG-1001
//nocontract:status 404
type ShopperNotFound struct {
	ShopperID int ` + "`json:\"shopper_id\"`" + `
}

func (ShopperNotFound) isReadOutcome() {}
`

// accountRoutes is a second package with one route, whose folder sorts before the ledger's.
const accountRoutes = `// Package account reads accounts.
package account

import (
	"context"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

// ReadOutcome is what reading an account ended as.
//
//sumtype:decl
type ReadOutcome interface{ isReadOutcome() }

// AccountRead is the account's name.
//
//ergo:not-an-error
//nocontract:status 200
type AccountRead struct {
	Name string ` + "`json:\"name\"`" + `
}

func (AccountRead) isReadOutcome() {}

// ReadAccount reads the account.
//
//nocontract:route GET /account
func ReadAccount(ctx context.Context, input nocontract.NoInput) ReadOutcome {
	return AccountRead{Name: "shop"}
}
`

// stopOn stops the test with the step's name and cause when the step failed.
func stopOn(t *testing.T, step string, failure error) {
	t.Helper()
	outcome.ForEachOption(outcome.EmptyableToOption(failure), func(cause error) { t.Fatalf("%s: %v", step, cause) })
}

// bytesOrStop is the content a step produced, or stops the test with the step's name and cause.
func bytesOrStop(t *testing.T, step string, produced outcome.Result[[]byte]) []byte {
	t.Helper()
	return outcome.Fold(produced, func(content []byte) []byte { return content }, func(cause error) []byte {
		t.Fatalf("%s: %v", step, cause)
		return []byte{}
	})
}

// pathOrStop is the path a step produced, or stops the test with the step's name and cause.
func pathOrStop(t *testing.T, step string, produced outcome.Result[string]) string {
	t.Helper()
	return outcome.Fold(produced, func(path string) string { return path }, func(cause error) string {
		t.Fatalf("%s: %v", step, cause)
		return ""
	})
}

// writeScratchFile writes content to the file at the slash-separated path under directory.
func writeScratchFile(t *testing.T, directory string, path string, content []byte) {
	t.Helper()
	file := filepath.Join(directory, filepath.FromSlash(path))
	stopOn(t, "making the folder of "+file, os.MkdirAll(filepath.Dir(file), 0o755))
	stopOn(t, "writing "+file, os.WriteFile(file, content, 0o644))
}

// scratchModFile is the shop's go.mod with nocontract read from checkout.
func scratchModFile(shopModFile []byte, checkout string) outcome.Result[[]byte] {
	return outcome.Fold(outcome.TupleToResult(modfile.Parse("go.mod", shopModFile, nil)), func(module *modfile.File) outcome.Result[[]byte] {
		return outcome.FoldOption(outcome.EmptyableToOption(module.AddReplace("github.com/CheckPickerUpper/nocontract", "", checkout, "")), outcome.Err[[]byte],
			func() outcome.Result[[]byte] { return outcome.TupleToResult(module.Format()) })
	}, outcome.Err[[]byte])
}

// scratchLedger is a project holding the ledger package, in a scratch module that reads this
// checkout of nocontract, built as the example shop's module is.
func scratchLedger(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	shop := filepath.Join("..", "testdata", "go", "shop")
	shopModFile := bytesOrStop(t, "reading the shop's go.mod", outcome.TupleToResult(os.ReadFile(filepath.Join(shop, "go.mod"))))
	checkout := pathOrStop(t, "finding this checkout", outcome.TupleToResult(filepath.Abs("..")))
	writeScratchFile(t, directory, "go.mod", bytesOrStop(t, "making the scratch go.mod", scratchModFile(shopModFile, checkout)))
	writeScratchFile(t, directory, "go.sum", bytesOrStop(t, "reading the shop's go.sum", outcome.TupleToResult(os.ReadFile(filepath.Join(shop, "go.sum")))))
	writeScratchFile(t, directory, "nocontract.json", []byte(`{"language": "go", "packages": ["./..."], "typescript": "web/ledger_api.generated.ts", "api": "LedgerApi", `+
		`"openapi": {"file": "ledger_api.openapi.json", "version": "1.0.0"}}`))
	writeScratchFile(t, directory, "ledger/ledger_routes.go", []byte(ledgerRoutes))
	return directory
}

// generate runs generate on the project and fails the test unless it wrote files.
func generate(t *testing.T, directory string) {
	t.Helper()
	generated := running.Run(running.Request{Arguments: []string{"generate", "-project", filepath.Join(directory, "nocontract.json")}, Context: t.Context()})
	if _, isWritten := generated.(running.FilesWritten); !isWritten {
		t.Fatalf("generate answered %v; want files written", generated.Lines())
	}
}

// requireCurrent runs RequireCurrent on the project from this package's folder, and is every
// message it failed with.
func requireCurrent(t *testing.T, directory string) []string {
	t.Helper()
	here := pathOrStop(t, "finding this package's folder", outcome.TupleToResult(filepath.Abs(".")))
	relative := pathOrStop(t, "finding the project file from this package's folder", outcome.TupleToResult(filepath.Rel(here, filepath.Join(directory, "nocontract.json"))))
	recorder := &failureRecorder{testContext: t.Context(), output: &strings.Builder{}, failures: []string{}}
	checking.RequireCurrent(recorder, filepath.ToSlash(relative))
	return recorder.failures
}

// A developer adds a failure to a handler's outcome and does not run generate: the test generate
// wrote into the route package fails, naming each file that differs, until generate runs again.
func TestAFailureAddedWithoutGeneratingFailsTheProjectsTest(t *testing.T) {
	t.Parallel()
	directory := scratchLedger(t)
	generate(t, directory)

	written := bytesOrStop(t, "reading the test generate writes into the ledger package",
		outcome.TupleToResult(os.ReadFile(filepath.Join(directory, "ledger", "nocontract_files_test.go"))))
	wantTest := "// Code generated by nocontract. DO NOT EDIT.\n\npackage ledger_test\n\nimport (\n\t\"testing\"\n\n\t\"github.com/CheckPickerUpper/nocontract/checking\"\n)\n\n" +
		"// TestNocontractFilesCurrent fails when a file nocontract generates differs from what `go tool nocontract generate` writes now.\n" +
		"func TestNocontractFilesCurrent(t *testing.T) {\n\tchecking.RequireCurrent(t, \"../nocontract.json\")\n}\n"
	if string(written) != wantTest {
		t.Fatalf("generate wrote the ledger's test as\n%s\nwant\n%s", written, wantTest)
	}

	writeScratchFile(t, directory, "ledger/shopper_missing.go", []byte(shopperMissing))
	stale := requireCurrent(t, directory)
	wantLines := []string{
		"NC-1034 These files differ from what nocontract generate writes; run it again:",
		filepath.Join(directory, "ledger", "nocontract_routes.go"),
		filepath.Join(directory, "ledger_api.openapi.json"),
		filepath.Join(directory, "web", "ledger_api.generated.ts"),
		"Run go tool nocontract generate to write the generated files again.",
	}
	if want := []string{strings.Join(wantLines, "\n") + "\n"}; !slices.Equal(stale, want) {
		t.Fatalf("RequireCurrent failed with %q; want %q", stale, want)
	}

	generate(t, directory)
	if regenerated := requireCurrent(t, directory); len(regenerated) != 0 {
		t.Fatalf("RequireCurrent failed with %q after generate; want no failure", regenerated)
	}
}

// staleRuntimeRoutes is a generated file written against a runtime that declared a name the
// runtime no longer declares.
const staleRuntimeRoutes = `// Code generated by nocontract. DO NOT EDIT.

package ledger

import nocontract "github.com/CheckPickerUpper/nocontract/go"

// Serving is what this package's routes are served with.
type Serving struct {
	Shopper nocontract.RetiredGuard
}
`

// ledgerCommand is a command importing the ledger, so Go compiles the ledger to type-check it.
const ledgerCommand = `// Command ledger reads balances.
package main

import "example.com/shop/ledger"

func main() { _ = ledger.ReadInput{ShopperID: 7} }
`

// A project upgraded to a runtime that no longer declares what its generated file names is read
// all the same: generate writes the file again, and check then finds every file current.
func TestAGeneratedFileThatNoLongerCompilesIsWrittenAgain(t *testing.T) {
	t.Parallel()
	directory := scratchLedger(t)
	generate(t, directory)
	writeScratchFile(t, directory, "ledger/nocontract_routes.go", []byte(staleRuntimeRoutes))
	writeScratchFile(t, directory, "cmd/ledger/main.go", []byte(ledgerCommand))

	generate(t, directory)
	project := filepath.Join(directory, "nocontract.json")
	checked := running.Run(running.Request{Arguments: []string{"check", "-project", project}, Context: t.Context()})
	if _, isCurrent := checked.(running.FilesCurrent); !isCurrent {
		t.Fatalf("check answered %v after generate; want every file current", checked.Lines())
	}
}

// A route package whose folder sorts before the one holding the test takes the test, and generate
// deletes it from the package that held it.
func TestTheTestMovesToTheRoutePackageThatSortsFirst(t *testing.T) {
	t.Parallel()
	directory := scratchLedger(t)
	generate(t, directory)
	writeScratchFile(t, directory, "account/account_routes.go", []byte(accountRoutes))
	generate(t, directory)

	written := bytesOrStop(t, "reading the test generate writes into the account package",
		outcome.TupleToResult(os.ReadFile(filepath.Join(directory, "account", "nocontract_files_test.go"))))
	if !strings.Contains(string(written), "package account_test\n") {
		t.Fatalf("generate wrote the account's test as\n%s\nwant it in package account_test", written)
	}
	ledgerTest := filepath.Join(directory, "ledger", "nocontract_files_test.go")
	outcome.Fold(outcome.TupleToResult(os.Stat(ledgerTest)), func(fs.FileInfo) struct{} {
		t.Fatalf("%s is still there; want generate to have deleted it", ledgerTest)
		return struct{}{}
	}, func(cause error) struct{} {
		if !errors.Is(cause, fs.ErrNotExist) {
			t.Fatalf("reading %s: %v; want it deleted", ledgerTest, cause)
		}
		return struct{}{}
	})
}
