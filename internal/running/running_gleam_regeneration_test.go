package running_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/outcome"
)

func TestGleamRegeneratesAnOutcomeWhileItsStartupImportsTheOldBinding(t *testing.T) {
	checkout := scratchproject.PathOrStop(t, "locating NoContract", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	fixture := readShopFixture(t, filepath.Join(checkout, "testdata", "gleam", "shop"))
	project := copyGleamShop(t, fixture)
	generate := func() {
		t.Helper()
		generated := running.Run(running.Request{Arguments: []string{"generate", "-project", project}, Context: t.Context()})
		if generated.ExitCode() != 0 {
			t.Fatalf("generate exited %d:\n%s", generated.ExitCode(), strings.Join(generated.Lines(), "\n"))
		}
	}
	generate()
	directory := filepath.Dir(project)
	startup := heldOrStop(t, filepath.Join(directory, "dev", "shop_server.gleam"))
	scratchproject.WriteFile(t, directory, "src/shop_server.gleam", []byte(startup))
	scratchproject.StopOn(t, "moving the startup into src", os.Remove(filepath.Join(directory, "dev", "shop_server.gleam")))
	for _, path := range []string{"src/balance.gleam", "src/shop/top_up_outcome.gleam"} {
		source := heldOrStop(t, filepath.Join(directory, filepath.FromSlash(path)))
		scratchproject.WriteFile(t, directory, path, []byte(strings.ReplaceAll(source, "TopUpQueued", "TopUpAccepted")))
	}
	checked := running.Run(running.Request{Arguments: []string{"check", "-project", project}, Context: t.Context()})
	if _, stale := checked.(running.FilesStale); !stale {
		t.Fatalf("check returned %T:\n%s; want FilesStale for the old constructor", checked, strings.Join(checked.Lines(), "\n"))
	}
	generate()
	build := exec.CommandContext(t.Context(), "gleam", "build", "--warnings-as-errors")
	build.Dir = directory
	compiled := &bytes.Buffer{}
	build.Stdout, build.Stderr = compiled, compiled
	buildFailure := build.Run()
	scratchproject.StopOn(t, "compiling the regenerated application:\n"+compiled.String(), buildFailure)
	t.Logf("regenerated application:\n%s", compiled)
}
