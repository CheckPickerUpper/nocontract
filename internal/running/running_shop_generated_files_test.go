package running_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

type shopSourceReplacement struct {
	path   string
	before string
	after  string
}

func TestShopProjectTestsRefuseStaleGeneratedFiles(t *testing.T) {
	checkout := scratchproject.PathOrStop(t, "locating NoContract", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	odinCommand := append([]string{"test", "generated/server", "-collection:nocontract=" + checkout}, compiler.Flags([]string{"nocontract_server", "filecheck"})...)
	odinCommand = append(odinCommand, "-out:.scratch/nocontract-files-test")
	for _, backend := range []struct {
		language     string
		prepare      func(*testing.T, shopFixture) string
		replacements []shopSourceReplacement
		executable   string
		arguments    []string
		checker      string
		stale        []string
	}{
		{
			language: "go",
			prepare:  copyShop,
			replacements: []shopSourceReplacement{{
				path: "balance/balance_routes.go", before: "nocontract:status 202", after: "nocontract:status 201",
			}},
			executable: "go",
			arguments:  []string{"test", "./..."},
			checker:    "balance/nocontract_files_test.go",
			stale:      []string{"balance/nocontract_routes.go", "shop_api.openapi.json", "web/src/shop_api.generated.ts"},
		},
		{
			language: "gleam",
			prepare:  copyGleamShop,
			replacements: []shopSourceReplacement{{
				path: "src/shop/read_outcome.gleam", before: "nocontract:status 200", after: "nocontract:status 201",
			}},
			executable: "gleam",
			arguments:  []string{"test"},
			checker:    "test/nocontract_files_test.gleam",
			stale:      []string{"src/nocontract/server.gleam", "shop_api.openapi.json", "web/src/shop_api.generated.ts"},
		},
		{
			language: "odin",
			prepare:  copyOdinTestShop,
			replacements: []shopSourceReplacement{
				{path: "balance/balance_routes.odin", before: "BalanceRead :: struct #all_or_none {", after: "BalanceRead :: struct #all_or_none {\n\treference: string,"},
				{path: "balance/balance_routes.odin", before: "return BalanceRead{", after: "return BalanceRead{\n\t\treference = \"balance-7\","},
			},
			executable: "odin",
			arguments:  odinCommand,
			checker:    "generated/server/nocontract_files_test.odin",
			stale:      []string{"generated/server/nocontract_bindings.odin", "shop_api.openapi.json", "web/src/shop_api.generated.ts"},
		},
	} {
		t.Run(backend.language, func(t *testing.T) {
			fixture := readShopFixture(t, filepath.Join(checkout, "testdata", backend.language, "shop"))
			project := backend.prepare(t, fixture)
			directory := filepath.Dir(project)
			module := heldOrStop(t, filepath.Join(directory, "go.mod"))
			scratchproject.WriteFile(t, directory, "go.mod", scratchproject.BytesOrStop(t, "pointing the Go tool at NoContract", scratchproject.LocalModule([]byte(module), checkout)))
			checkedProject := filepath.Join(directory, `contract ; literal's "quoted".json`)
			scratchproject.StopOn(t, "naming the checked project", os.Rename(project, checkedProject))
			checker := filepath.Join(directory, filepath.FromSlash(backend.checker))
			removed := os.Remove(checker)
			if !errors.Is(removed, os.ErrNotExist) {
				scratchproject.StopOn(t, "clearing the fixture's generated test", removed)
			}
			generate := func(t *testing.T) {
				t.Helper()
				generated := running.Run(running.Request{Arguments: []string{"generate", "-project", checkedProject}, Context: t.Context()})
				if generated.ExitCode() != 0 {
					t.Fatalf("generation exited %d:\n%s", generated.ExitCode(), strings.Join(generated.Lines(), "\n"))
				}
			}
			generate(t)
			checkerSource := heldOrStop(t, checker)
			replaceShopSources(t, directory, backend.replacements)
			paths := []string{}
			for _, path := range backend.stale {
				paths = append(paths, filepath.Join(directory, filepath.FromSlash(path)))
			}
			stale := running.FilesStale{Paths: paths}
			for _, stage := range []struct {
				name       string
				prepare    func(*testing.T)
				exitStatus int
				report     []string
			}{
				{name: "control without generated test", prepare: func(t *testing.T) {
					scratchproject.StopOn(t, "removing the emitted test for the control run", os.Remove(checker))
				}, exitStatus: 0, report: []string{}},
				{name: "stale generated files", prepare: func(t *testing.T) {
					scratchproject.WriteFile(t, directory, backend.checker, []byte(checkerSource))
				}, exitStatus: 1, report: stale.Lines()},
				{name: "regenerated files", prepare: generate, exitStatus: 0, report: []string{}},
			} {
				passed := t.Run(stage.name, func(t *testing.T) {
					stage.prepare(t)
					command := exec.CommandContext(t.Context(), backend.executable, backend.arguments...)
					command.Dir = directory
					output := &bytes.Buffer{}
					command.Stdout, command.Stderr = output, output
					status := outcome.Fold(outcome.TupleToResult(struct{}{}, command.Run()), func(struct{}) int { return 0 }, func(cause error) int {
						return outcome.FoldOption(outcome.TupleToOption(errors.AsType[*exec.ExitError](cause)), func(exited *exec.ExitError) int { return exited.ExitCode() }, func() int {
							t.Fatalf("%v in %s could not start: %v\n%s", command.Args, directory, cause, output)
							return -1
						})
					})
					if status != stage.exitStatus {
						t.Fatalf("%v in %s exited %d; want %d:\n%s", command.Args, directory, status, stage.exitStatus, output)
					}
					lines := []string{}
					for _, line := range strings.Split(output.String(), "\n") {
						line = strings.TrimSpace(line)
						if strings.HasPrefix(line, "NC-") || strings.HasPrefix(line, directory+string(filepath.Separator)) {
							lines = append(lines, line)
						}
					}
					slices.Sort(lines)
					lines = slices.Compact(lines)
					slices.Sort(stage.report)
					if !slices.Equal(lines, stage.report) {
						t.Fatalf("reported %q; want %q:\n%s", lines, stage.report, output)
					}
					t.Logf("%v exited %d:\n%s", command.Args, status, output)
				})
				if !passed {
					return
				}
			}
		})
	}
}

func replaceShopSources(t *testing.T, directory string, replacements []shopSourceReplacement) {
	t.Helper()
	for _, replacement := range replacements {
		original := heldOrStop(t, filepath.Join(directory, filepath.FromSlash(replacement.path)))
		changed := strings.Replace(original, replacement.before, replacement.after, 1)
		if changed == original {
			t.Fatalf("%s has no %q to change", replacement.path, replacement.before)
		}
		scratchproject.WriteFile(t, directory, replacement.path, []byte(changed))
	}
}

func copyOdinTestShop(t *testing.T, fixture shopFixture) string {
	t.Helper()
	project := copyShop(t, fixture)
	directory := filepath.Dir(project)
	replaceShopSources(t, directory, []shopSourceReplacement{{path: "nocontract.json", before: `"server": "server"`, after: `"server": "generated/server"`}})
	scratchproject.WriteFile(t, directory, "generated/server/shop_test.odin", []byte(`#+test
package nocontract_server

import balance "../../balance"
import "core:fmt"
import "core:testing"

@(test)
shopper_balance_is_read :: proc(t: ^testing.T) {
	_ = serve
	switch answer in balance.ReadBalance(balance.ReadInput{shopper_id = 7, currency = .CAD}) {
	case balance.BalanceRead:
		testing.expect_value(t, answer.balance.cents, 1250)
	case balance.ShopperNotFound, balance.BalanceNotOpened:
		testing.expect(t, false, fmt.tprintf("%v", answer))
	}
}
`))
	return project
}
