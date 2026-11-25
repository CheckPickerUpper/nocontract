package running_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

// The supervisor program re-executes itself as its worker, so each scenario runs as its own
// process and the program checks its own supervision outcome before printing "proved".
func TestOdinSupervisorScenariosProveTheirOutcome(t *testing.T) {
	checkout := scratchproject.PathOrStop(t, "locating the checkout", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	binary := filepath.Join(t.TempDir(), "supervisor-proof")
	arguments := append([]string{"build", filepath.Join(checkout, "odin", "runtime", "supervisor", "testprogram"), "-out:" + binary}, compiler.Flags([]string{"main", "supervisor"})...)
	runShopCommand(t, exec.CommandContext(t.Context(), "odin", arguments...))
	scenarios := []string{"Restart", "Startup", "Startup_Crash", "Normal_Nonzero", "Stop_After_Crash", "Final_Notification", "Held_Shutdown", "Invalid_Shutdown_Period"}
	for _, signal := range []string{"SIGINT", "SIGTERM"} {
		for _, scenario := range scenarios {
			t.Run(scenario+"/"+signal, func(t *testing.T) {
				proof := exec.CommandContext(t.Context(), binary, scenario, signal, "original argv")
				proof.Env = append(os.Environ(), "NOCONTRACT_PROOF_ENV=inherited environment")
				printed := string(runShopCommand(t, proof))
				if !strings.HasPrefix(printed, "proved "+scenario+" ") {
					t.Fatalf("%s %s printed %q", scenario, signal, printed)
				}
				t.Log(strings.TrimSpace(printed))
			})
		}
	}
}
