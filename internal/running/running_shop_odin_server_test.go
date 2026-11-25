package running_test

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/sys/unix"
)

func openOdinShop(t *testing.T, project string) shopServerAddress {
	t.Helper()
	return openOdinShopWithExitCheck(t, project, os.Interrupt, func(cause error) {
		t.Errorf("waiting for the Odin shop's interrupt shutdown: %v", cause)
	})
}

// openOdinShopWithExitCheck starts the shop and, when the test finishes it, sends stop to the
// supervisor and hands the supervisor's exit to verify.
func openOdinShopWithExitCheck(t *testing.T, project string, stop os.Signal, verify func(error)) shopServerAddress {
	t.Helper()
	built := buildOdinShop(t, project)
	lifetime, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
	t.Cleanup(cancel)
	command := exec.CommandContext(lifetime, built.binary)
	command.Dir = built.directory
	return openShopProcess(t, command, shopProcessShutdown{
		stop: func() {
			outcome.ForEachOption(outcome.EmptyableToOption(command.Process.Signal(stop)), func(cause error) {
				if !errors.Is(cause, os.ErrProcessDone) {
					t.Errorf("sending %v to the Odin shop: %v", stop, cause)
				}
			})
		},
		verify: verify,
	})
}

type builtOdinShop struct {
	directory string
	binary    string
}

// buildOdinShop moves the generated shop out of the checkout and compiles it.
func buildOdinShop(t *testing.T, project string) builtOdinShop {
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
	return builtOdinShop{directory: directory, binary: binary}
}

func generateOdinShop(t *testing.T) string {
	t.Helper()
	checkout := scratchproject.PathOrStop(t, "locating the checkout", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	project := copyShop(t, readShopFixture(t, filepath.Join(checkout, "testdata", "odin", "shop")))
	generated := running.Run(running.Request{Arguments: []string{"generate", "-project", project}, Context: t.Context()})
	if generated.ExitCode() != 0 {
		t.Fatalf("generating the Odin shop: %s", strings.Join(generated.Lines(), "\n"))
	}
	return project
}

// odinShopOpening is a shop that fails before any worker reports ready, and what it must print.
type odinShopOpening struct {
	name     string
	env      []string
	failure  string
	evidence []string
	workers  int
}

func TestOdinShopThatCannotOpenReturnsListenerNotOpened(t *testing.T) {
	built := buildOdinShop(t, generateOdinShop(t))
	occupied := outcome.Fold(outcome.TupleToResult(net.Listen("tcp", "127.0.0.1:0")), func(listener net.Listener) net.Listener { return listener }, func(cause error) net.Listener {
		t.Fatalf("occupying a port: %v", cause)
		return nil
	})
	t.Cleanup(func() { scratchproject.StopOn(t, "closing the occupied port", occupied.Close()) })
	port := strconv.Itoa(occupied.Addr().(*net.TCPAddr).Port)
	openings := []odinShopOpening{
		{
			name: "occupied port", env: []string{"SHOP_TEST_LISTEN_PORT=" + port},
			failure:  "Listener_Not_Opened{requested = Requested_Endpoint{value = Endpoint{address = [127, 0, 0, 1], port = " + port + "}}, failure = Listener_Not_Bound{",
			evidence: []string{`cause = "Address_In_Use", errno = "EADDRINUSE"`}, workers: 0,
		},
		{
			name: "worker crash before ready", env: []string{"SHOP_TEST_STARTUP_CRASH=supervisor"},
			failure:  "Listener_Not_Opened{requested = Requested_Endpoint{value = Endpoint{address = [127, 0, 0, 1], port = 0}}, failure = Worker_Exited{termination = Termination{",
			evidence: []string{`kind = Signaled{signal = "SIGSEGV"`}, workers: 1,
		},
	}
	for _, opening := range openings {
		t.Run(opening.name, func(t *testing.T) {
			lifetime, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(lifetime, built.binary)
			command.Dir = built.directory
			command.Env = append(os.Environ(), opening.env...)
			output, runError := command.CombinedOutput()
			exited, isExit := errors.AsType[*exec.ExitError](runError)
			if !isExit || exited.ExitCode() != 1 {
				t.Fatalf("the shop ended with %v, want exit 1:\n%s", runError, output)
			}
			failed := ""
			for line := range strings.Lines(string(output)) {
				if reported, found := strings.CutPrefix(line, "NoContract server failed: "); found {
					failed = reported
				}
			}
			if !strings.HasPrefix(failed, opening.failure) {
				t.Fatalf("the shop failed with %q, want %s:\n%s", failed, opening.failure, output)
			}
			for _, evidence := range opening.evidence {
				if !strings.Contains(failed, evidence) {
					t.Fatalf("the shop failure omitted %s:\n%s", evidence, output)
				}
			}
			if started := strings.Count(string(output), "worker_started pid="); started != opening.workers {
				t.Fatalf("the shop started %d workers, want %d:\n%s", started, opening.workers, output)
			}
			t.Logf("the shop returned NC-1140 Listener_Not_Opened after starting %d workers", opening.workers)
		})
	}
}

func TestOdinWorkerEndsWithItsKilledSupervisor(t *testing.T) {
	served := openOdinShopWithExitCheck(t, generateOdinShop(t), syscall.SIGKILL, func(cause error) {
		if exited, matched := errors.AsType[*exec.ExitError](cause); !matched || exited.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Errorf("the supervisor ended with %v, want SIGKILL", cause)
		}
	})
	address := strings.TrimPrefix(served.url, "http://")
	worker := odinWorkerPID(t, served)
	// The pidfd is opened while the supervisor still holds the worker, so it names that process.
	pidfd := intOrStop(t, "opening the worker's pidfd", outcome.TupleToResult(unix.PidfdOpen(worker, 0)))
	defer func() { scratchproject.StopOn(t, "closing the worker's pidfd", unix.Close(pidfd)) }()
	served.finish()
	// A pidfd turns readable when its process has exited, after the kernel released its files.
	exited := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN, Revents: 0}}
	if ready, pollError := unix.Poll(exited, int((10 * time.Second).Milliseconds())); pollError != nil || ready != 1 {
		t.Fatalf("worker %d outlived its killed supervisor: ready=%d %v", worker, ready, pollError)
	}
	accepted := awaitRefusedConnections(t, address)
	t.Logf("worker %d ended with its killed supervisor and %s refused connections after %d more accepts", worker, address, accepted)
}

// awaitRefusedConnections dials until the address refuses and counts the dials it accepted.
// The kernel tears a dead worker's io_uring down on a work queue with no event a test can wait
// on, and until then its pending accept keeps the listening socket open.
func awaitRefusedConnections(t *testing.T, address string) int {
	t.Helper()
	deadline, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	dialer := net.Dialer{}
	accepted := 0
	for {
		connection, dialError := dialer.DialContext(deadline, "tcp", address)
		switch {
		case dialError == nil:
			accepted++
			scratchproject.StopOn(t, "closing a connection the dying listener accepted", connection.Close())
		case errors.Is(dialError, syscall.ECONNREFUSED):
			return accepted
		default:
			t.Fatalf("dialing %s after the supervisor and worker ended: %v after %d accepts", address, dialError, accepted)
			return accepted
		}
	}
}

// odinWorkerPID is the supervisor's one child. The shop prints its address once a worker is ready,
// so the worker exists when the test reads it.
func odinWorkerPID(t *testing.T, served shopServerAddress) int {
	t.Helper()
	supervisor := strconv.Itoa(served.pid)
	children := scratchproject.BytesOrStop(t, "listing the supervisor's children", outcome.TupleToResult(os.ReadFile(filepath.Join("/proc", supervisor, "task", supervisor, "children"))))
	listed := strings.Fields(string(children))
	if len(listed) != 1 {
		t.Fatalf("the supervisor has children %v, want one worker", listed)
	}
	return intOrStop(t, "reading the worker pid "+listed[0], outcome.TupleToResult(strconv.Atoi(listed[0])))
}

func intOrStop(t *testing.T, step string, produced outcome.Result[int]) int {
	t.Helper()
	return outcome.Fold(produced, func(value int) int { return value }, func(cause error) int {
		t.Fatalf("%s: %v", step, cause)
		return 0
	})
}
