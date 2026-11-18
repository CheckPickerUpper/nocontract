// Package scratchproject writes small NoContract projects into a test's temporary folder, each a
// Go module that reads a checkout of nocontract, for tests that run generate and check on them.
package scratchproject

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/mod/modfile"
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

// Ledger is a project holding the ledger package, in a scratch module that reads the nocontract
// checkout at the relative path checkout, built as the example shop's module is.
func Ledger(t testing.TB, checkout string) string {
	t.Helper()
	directory := t.TempDir()
	root := PathOrStop(t, "finding the checkout", outcome.TupleToResult(filepath.Abs(checkout)))
	shop := filepath.Join(root, "testdata", "go", "shop")
	shopModFile := BytesOrStop(t, "reading the shop's go.mod", outcome.TupleToResult(os.ReadFile(filepath.Join(shop, "go.mod"))))
	WriteFile(t, directory, "go.mod", BytesOrStop(t, "making the scratch go.mod", modFile(shopModFile, root)))
	WriteFile(t, directory, "go.sum", BytesOrStop(t, "reading the shop's go.sum", outcome.TupleToResult(os.ReadFile(filepath.Join(shop, "go.sum")))))
	WriteFile(t, directory, "nocontract.json", []byte(`{"language": "go", "packages": ["./..."], "typescript": "web/ledger_api.generated.ts", "api": "LedgerApi", `+
		`"openapi": {"file": "ledger_api.openapi.json", "version": "1.0.0"}, "server": "server"}`))
	WriteFile(t, directory, "ledger/ledger_routes.go", []byte(LedgerRoutes))
	return directory
}
