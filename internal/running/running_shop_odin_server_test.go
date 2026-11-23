package running_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

func openOdinShop(t *testing.T, project string) shopServerAddress {
	t.Helper()
	relocated := filepath.Join(t.TempDir(), "shop")
	scratchproject.StopOn(t, "moving the generated shop to "+relocated, os.Rename(filepath.Dir(project), relocated))
	directory := relocated
	checkout := scratchproject.PathOrStop(t, "locating the runtime source collection", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	binary := filepath.Join(t.TempDir(), "shop")
	arguments := append([]string{"build", filepath.Join(directory, "cmd", "shop"), "-out:" + binary, "-collection:nocontract=" + checkout}, compiler.Flags([]string{"main", "nocontract_server"})...)
	build := exec.CommandContext(t.Context(), "odin", arguments...)
	build.Dir = directory
	runShopCommand(t, build)
	lifetime, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
	t.Cleanup(cancel)
	command := exec.CommandContext(lifetime, binary)
	command.Dir = directory
	return openShopProcess(t, command, shopProcessShutdown{
		stop: func() {
			outcome.ForEachOption(outcome.EmptyableToOption(command.Process.Signal(os.Interrupt)), func(cause error) {
				if !errors.Is(cause, os.ErrProcessDone) {
					t.Errorf("interrupting the Odin shop: %v", cause)
				}
			})
		},
		verify: func(cause error) { t.Errorf("waiting for the Odin shop's interrupt shutdown: %v", cause) },
	})
}
