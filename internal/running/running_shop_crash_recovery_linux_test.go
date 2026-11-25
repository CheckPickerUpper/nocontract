package running_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/outcome"
)

func TestShopBackendsRecoverCrashedRequestsAtTheSameAddress(t *testing.T) {
	checkout := scratchproject.PathOrStop(t, "locating the checkout", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	expected := readShopGeneratedFiles(t, filepath.Join(checkout, "testdata", "go", "shop", "nocontract.json"))
	contract := readShopContract(t, expected.document)
	client := prepareShopClient(t, checkout, expected.client)
	declared := `{"method":"GET","path":"/crashes/{cause}"}`
	crashes := []shopCrash{
		{cause: "panic", path: "/crashes/panic", route: declared},
		{cause: "bounds", path: "/crashes/bounds", route: declared},
		{cause: "layer-before", path: "/crashes/layer-before", route: declared},
		{cause: "layer-after", path: "/crashes/layer-after", route: declared},
		{cause: "unmatched", path: "/unmatched-crash", route: "null"},
	}
	for _, backend := range shopBackends() {
		t.Run(backend.language, func(t *testing.T) {
			control := listenForHeldShopRequests(t)
			served := openCheckedShop(t, checkout, backend, expected)
			for _, crash := range crashes {
				t.Run(crash.cause, func(t *testing.T) {
					proof, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					held := holdShopTopUp(t, proof, served, control)
					request := crashShopRequest(crash)
					crashed := served.notices.mark()
					answer := sendShopRequest(t, served, request)
					assertShopAnswer(t, request, answer)
					assertShopCrashSchemas(t, contract, answer)
					if backend.language == "odin" {
						awaitShopReplacement(t, proof, served, crashed)
						assertCrashedWorkerDisconnected(t, held)
						t.Log("the Odin worker's other in-flight top-up lost its connection")
					} else {
						completeHeldShopTopUp(t, held)
						t.Log("the other in-flight top-up completed with 202 TopUpQueued")
					}
					assertShopAnswersAfterCrash(t, served)
					t.Logf("%s %s answered %d %s", backend.language, crash.cause, answer.status, answer.body)
				})
			}

			decoded := exec.CommandContext(t.Context(), "bun", "run", "--no-install", "src/request_crash.ts")
			decoded.Dir = client
			decoded.Env = append(os.Environ(), "SHOP_URL="+served.url)
			crashed := served.notices.mark()
			t.Logf("Effect decoded %s", strings.TrimSpace(string(runShopCommand(t, decoded))))
			if backend.language == "odin" {
				proof, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				awaitShopReplacement(t, proof, served, crashed)
				assertOdinShopCrashDiagnostics(t, served.finish(), len(crashes)+2)
			}
		})
	}
}

// Each shop runs its handlers under a request deadline here, so a crash happens inside the
// deadline's worker: a Gleam OTP process, an Odin deadline thread or a Go goroutine. The deadline
// is far longer than a crash takes, so the crash always answers first.
func TestShopBackendsAnswerCrashesUnderTheirRequestDeadline(t *testing.T) {
	t.Setenv("SHOP_TEST_REQUEST_DEADLINE", "10000")
	checkout := scratchproject.PathOrStop(t, "locating the checkout", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	expected := readShopGeneratedFiles(t, filepath.Join(checkout, "testdata", "go", "shop", "nocontract.json"))
	declared := `{"method":"GET","path":"/crashes/{cause}"}`
	crashes := []shopCrash{
		{cause: "panic", path: "/crashes/panic", route: declared},
		{cause: "layer-before", path: "/crashes/layer-before", route: declared},
	}
	for _, backend := range shopBackends() {
		t.Run(backend.language, func(t *testing.T) {
			served := openCheckedShop(t, checkout, backend, expected)
			for _, crash := range crashes {
				request := crashShopRequest(crash)
				crashed := served.notices.mark()
				assertShopAnswer(t, request, sendShopRequest(t, served, request))
				if backend.language == "odin" {
					proof, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					awaitShopReplacement(t, proof, served, crashed)
					cancel()
				}
			}
			written := strings.Join(served.finish(), "\n")
			for _, crash := range crashes {
				if !strings.Contains(written, `"path":"`+crash.path+`"`) {
					t.Fatalf("%s logged no crash for %s:\n%s", backend.language, crash.path, written)
				}
			}
			t.Logf("%s answered and logged each crash under its request deadline", backend.language)
		})
	}
}

// shopCrash is one deliberate crash the shops raise, and the route its refusal reports.
type shopCrash struct {
	cause string
	path  string
	route string
}

func crashShopRequest(crash shopCrash) shopRequestCase {
	return shopRequestCase{
		name: "deliberate " + crash.cause, method: http.MethodGet, path: crash.path,
		shopper: "", body: "", headers: []shopRequestHeader{}, bodySchema: outcome.None[shopBodySchema](),
		want: shopAnswer{status: http.StatusInternalServerError, allow: "", body: `{"_tag":"RequestCrashed","code":"NC-1172","route":` + crash.route + `}`},
	}
}

func assertShopCrashSchemas(t *testing.T, contract shopContract, answer shopAnswer) {
	t.Helper()
	for path, methods := range contract.document.Paths {
		for method, operation := range methods {
			if _, declared := operation.Responses["500"]; !declared {
				t.Fatalf("%s %s does not declare the crash response", method, path)
			}
			assertShopResponseValueSchema(t, contract, answer, "paths", path, method, "responses", "500", "content", "application/json", "schema")
		}
	}
}

// awaitShopReplacement waits for the first notice the shop printed after crashed, the mark taken
// before the request that crashed its worker.
func awaitShopReplacement(t *testing.T, proof context.Context, served shopServerAddress, crashed shopNoticeMark) {
	t.Helper()
	select {
	case notice := <-served.notices.after(crashed):
		switch notice := notice.(type) {
		case shopListening:
			if notice.address != served.url {
				t.Fatalf("replacement listening address = %q, want %s", notice.address, served.url)
			}
			served.client.CloseIdleConnections()
		case shopExited:
			t.Fatalf("the shop exited before its replacement Odin worker reported ready; its last standard error lines:\n%s", strings.Join(notice.stderr, "\n"))
		case nil:
		}
	case <-proof.Done():
		t.Fatalf("waiting for the replacement Odin worker: %v", proof.Err())
	}
}

func assertCrashedWorkerDisconnected(t *testing.T, held heldShopTopUp) {
	t.Helper()
	response, readError := http.ReadResponse(bufio.NewReader(held.connection), held.request)
	outcome.Fold(outcome.TupleToResult(response, readError), func(response *http.Response) struct{} {
		scratchproject.StopOn(t, "closing the old worker's response", response.Body.Close())
		t.Errorf("the old worker's held request answered %s", response.Status)
		return struct{}{}
	}, func(cause error) struct{} {
		if !errors.Is(cause, io.EOF) && !errors.Is(cause, io.ErrUnexpectedEOF) && !errors.Is(cause, syscall.ECONNRESET) {
			t.Errorf("the old worker's held request did not disconnect: %v", cause)
		}
		return struct{}{}
	})
}

func assertShopAnswersAfterCrash(t *testing.T, served shopServerAddress) {
	t.Helper()
	request := shopRequestCase{
		name: "normal request at the same address", method: http.MethodGet, path: "/crashes/normal",
		shopper: "", body: "", headers: []shopRequestHeader{}, bodySchema: outcome.None[shopBodySchema](),
		want: shopAnswer{status: http.StatusOK, allow: "", body: `{"_tag":"OrderCountRead","order_count":"7"}`},
	}
	assertShopAnswer(t, request, sendShopRequest(t, served, request))
	t.Log("the same address answered 200 OrderCountRead")
}

func assertOdinShopCrashDiagnostics(t *testing.T, lines []string, workerCount int) {
	t.Helper()
	written := strings.Join(lines, "\n")
	for _, evidence := range []string{`"event":"request_crashed"`, `"path":"/crashes/panic"`, `"path":"/crashes/bounds"`, `"path":"/crashes/layer-before"`, `"path":"/crashes/layer-after"`, `"path":"/unmatched-crash"`, `"route":"/crashes/{cause}"`, `"cause":"panic: deliberate shop panic","stack":"`, `"cause":"Bounds check failed: `, `signal = "SIGILL"`} {
		if !strings.Contains(written, evidence) {
			t.Fatalf("Odin crash diagnostics omitted %s:\n%s", evidence, written)
		}
	}
	if workers := odinWorkerPIDs(lines); len(workers) != workerCount {
		t.Fatalf("Odin started workers %v, want %d:\n%s", workers, workerCount, written)
	}
	t.Logf("Odin recorded each crashed request and used %d worker PIDs", workerCount)
}

// odinWorkerPIDs lists each distinct worker process the Odin supervisor reported ready.
func odinWorkerPIDs(lines []string) []string {
	workers := []string{}
	for _, line := range lines {
		if worker, found := strings.CutPrefix(line, "worker_ready pid="); found {
			pid, _, _ := strings.Cut(worker, " ")
			if !slices.Contains(workers, pid) {
				workers = append(workers, pid)
			}
		}
	}
	return workers
}
