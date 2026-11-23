package running_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

func TestOdinRoutesCanImportConfiguredCollections(t *testing.T) {
	directory := t.TempDir()
	scratchproject.WriteFile(t, directory, "nocontract.json", []byte(`{
	  "language": "odin",
	  "packages": ["balance"],
	  "collections": {"credentials": "application packages"},
	  "api": "ShopApi",
	  "openapi": {"file": "shop.openapi.json", "version": "1.0.0"},
	  "server": "server"
	}`))
	scratchproject.WriteFile(t, directory, "application packages/verification/verification.odin", []byte(`package verification
label :: "verified"
`))
	scratchproject.WriteFile(t, directory, "balance/balance.odin", []byte(`package balance

import "credentials:verification"

ReadInput :: struct #all_or_none {}

//nocontract:status 200
@(not_an_error)
BalanceRead :: struct #all_or_none { credential: string }

ReadOutcome :: union { BalanceRead }

//nocontract:route GET /balance
@(require_results)
Read :: proc(_input: ReadInput) -> ReadOutcome {
	return BalanceRead{credential = verification.label}
}
`))

	for _, command := range []string{"generate", "check"} {
		result := running.Run(running.Request{
			Arguments: []string{command, "-project", filepath.Join(directory, "nocontract.json")},
			Context:   t.Context(),
		})
		if result.ExitCode() != 0 {
			t.Fatalf("%s exited %d:\n%s", command, result.ExitCode(), strings.Join(result.Lines(), "\n"))
		}
	}
}
