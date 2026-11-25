package running_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/sync/errgroup"
)

func TestShopBackendsDrainAnActiveRequestOnShutdown(t *testing.T) {
	checkout := scratchproject.PathOrStop(t, "locating the checkout", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	for _, backend := range []shopBackend{
		{language: "go", prepare: copyGoShop, open: openGoShop},
		{language: "odin", prepare: copyShop, open: openOdinShop},
	} {
		t.Run(backend.language, func(t *testing.T) {
			fixture := readShopFixture(t, filepath.Join(checkout, "testdata", backend.language, "shop"))
			project := backend.prepare(t, fixture)
			generated := running.Run(running.Request{Arguments: []string{"generate", "-project", project}, Context: t.Context()})
			if generated.ExitCode() != 0 {
				t.Fatalf("generating the %s shop: %s", backend.language, strings.Join(generated.Lines(), "\n"))
			}
			holdRequests := listenForHeldShopRequests(t)
			assertShopRequestDrainsOnShutdown(t, backend.open(t, project), holdRequests)
		})
	}
}

func TestOdinShutdownReportsItsExpiredPeriod(t *testing.T) {
	checkout := scratchproject.PathOrStop(t, "locating the checkout", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	fixture := readShopFixture(t, filepath.Join(checkout, "testdata", "odin", "shop"))
	project := copyShop(t, fixture)
	generated := running.Run(running.Request{Arguments: []string{"generate", "-project", project}, Context: t.Context()})
	if generated.ExitCode() != 0 {
		t.Fatalf("generating the Odin shop: %s", strings.Join(generated.Lines(), "\n"))
	}
	control := listenForHeldShopRequests(t)
	t.Setenv("SHOP_TEST_SHORT_SHUTDOWN", "1")
	exits := []int{}
	served := openOdinShopWithExitCheck(t, project, os.Interrupt, func(cause error) {
		if exited, matched := errors.AsType[*exec.ExitError](cause); matched {
			exits = append(exits, exited.ExitCode())
			return
		}
		t.Errorf("waiting for the expired Odin shop: %v", cause)
	})
	proof, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	held := holdShopTopUp(t, proof, served, control)
	lines := served.finish()
	if len(exits) != 1 || exits[0] != 1 {
		t.Fatalf("the shop reported exits %v, want [1] after its deadline", exits)
	}
	workers := odinWorkerPIDs(lines)
	if len(workers) != 1 {
		t.Fatalf("the shop started workers %v, want one:\n%s", workers, strings.Join(lines, "\n"))
	}
	reaped := "stop = Worker_Reaped{state = Process_State{pid = " + workers[0] + ", exited = true,"
	for _, line := range lines {
		if strings.HasPrefix(line, "NoContract server failed: Listener_Not_Stopped{") && strings.Contains(line, reaped) {
			if _, statError := os.Stat(filepath.Join("/proc", workers[0])); !errors.Is(statError, fs.ErrNotExist) {
				t.Fatalf("the expired worker %s still has a process entry: %v", workers[0], statError)
			}
			assertCrashedWorkerDisconnected(t, held)
			t.Logf("the supervisor killed and waited for worker %s after the shutdown period: %s", workers[0], line)
			return
		}
	}
	t.Fatalf("the listener omitted the typed shutdown expiry of worker %s:\n%s", workers[0], strings.Join(lines, "\n"))
}

func listenForHeldShopRequests(t *testing.T) *net.TCPListener {
	t.Helper()
	listener := outcome.Fold(outcome.TupleToResult(net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0, Zone: ""})),
		func(listener *net.TCPListener) *net.TCPListener { return listener }, func(cause error) *net.TCPListener {
			t.Fatalf("opening the request hold listener: %v", cause)
			return &net.TCPListener{}
		})
	t.Cleanup(func() { scratchproject.StopOn(t, "closing the request hold listener", listener.Close()) })
	t.Setenv("SHOP_HOLD_REQUEST_ADDRESS", listener.Addr().String())
	return listener
}

type heldShopTopUp struct {
	request    *http.Request
	connection net.Conn
	control    *net.TCPConn
}

const heldShopTopUpBody = `{"amount":{"cents":"500","currency":"CAD"}}`

func holdShopTopUp(t *testing.T, proofContext context.Context, served shopServerAddress, holdRequests *net.TCPListener) heldShopTopUp {
	t.Helper()
	request := outcome.Fold(outcome.TupleToResult(http.NewRequestWithContext(proofContext, http.MethodPost, served.url+"/shoppers/7/top-ups", strings.NewReader(heldShopTopUpBody))),
		func(request *http.Request) *http.Request { return request }, func(cause error) *http.Request {
			t.Fatalf("building the held top-up request: %v", cause)
			return &http.Request{}
		})
	t.Cleanup(func() { scratchproject.StopOn(t, "closing the held request body", request.Body.Close()) })
	dialer := &net.Dialer{}
	connection := outcome.Fold(outcome.TupleToResult(dialer.DialContext(proofContext, "tcp", request.URL.Host)),
		func(connection net.Conn) net.Conn { return connection }, func(cause error) net.Conn {
			t.Fatalf("opening the held top-up connection: %v", cause)
			return &net.TCPConn{}
		})
	t.Cleanup(func() { scratchproject.StopOn(t, "closing the held top-up connection", connection.Close()) })
	deadline, _ := proofContext.Deadline()
	scratchproject.StopOn(t, "bounding the held top-up connection", connection.SetDeadline(deadline))
	scratchproject.StopOn(t, "bounding application entry", holdRequests.SetDeadline(deadline))
	outcome.Fold(outcome.TupleToResult(fmt.Fprintf(connection,
		"POST /shoppers/7/top-ups HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		request.URL.Host, len(heldShopTopUpBody), heldShopTopUpBody)), func(int) struct{} { return struct{}{} }, func(cause error) struct{} {
		t.Fatalf("sending the held top-up request: %v", cause)
		return struct{}{}
	})
	held := outcome.Fold(outcome.TupleToResult(holdRequests.AcceptTCP()), func(connection *net.TCPConn) *net.TCPConn { return connection },
		func(cause error) *net.TCPConn {
			t.Fatalf("waiting for the outer application layer to hold the request: %v", cause)
			return &net.TCPConn{}
		})
	t.Cleanup(func() { scratchproject.StopOn(t, "closing the held request's control connection", held.Close()) })
	scratchproject.StopOn(t, "bounding the held request's release", held.SetDeadline(deadline))
	return heldShopTopUp{request: request, connection: connection, control: held}
}

func assertShopRequestDrainsOnShutdown(t *testing.T, served shopServerAddress, holdRequests *net.TCPListener) {
	t.Helper()
	defer func() {
		if t.Failed() {
			t.Logf("Shop process output:\n%s", strings.Join(served.finish(), "\n"))
		}
	}()
	proofContext, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stopping := &errgroup.Group{}
	defer func() { scratchproject.StopOn(t, "joining the shop shutdown", stopping.Wait()) }()
	held := holdShopTopUp(t, proofContext, served, holdRequests)

	finished := make(chan []string, 1)
	stopping.Go(func() error { finished <- served.finish(); return nil })
	awaitShopListenerRefusal(t, proofContext, held.request.URL.Host)
	select {
	case lines := <-finished:
		t.Fatalf("the shop exited while its top-up was still held by the outer layer:\n%s", strings.Join(lines, "\n"))
	default:
	}
	completeHeldShopTopUp(t, held)
	select {
	case <-finished:
	case <-proofContext.Done():
		t.Fatalf("waiting for the shop to finish its shutdown: %v", proofContext.Err())
	}
}

func completeHeldShopTopUp(t *testing.T, held heldShopTopUp) {
	t.Helper()
	release := [1]byte{1}
	outcome.Fold(outcome.TupleToResult(held.control.Write(release[:])), func(int) struct{} { return struct{}{} }, func(cause error) struct{} {
		t.Fatalf("releasing the top-up: %v", cause)
		return struct{}{}
	})
	confirmed := [1]byte{}
	outcome.Fold(outcome.TupleToResult(io.ReadFull(held.control, confirmed[:])), func(int) struct{} { return struct{}{} }, func(cause error) struct{} {
		t.Fatalf("confirming that the application received the release byte: %v", cause)
		return struct{}{}
	})
	if confirmed != release {
		t.Fatalf("the application confirmed release with %v; want %v", confirmed, release)
	}
	response := readShopHTTPResponse(t, bufio.NewReader(held.connection), held.request)
	defer func() { scratchproject.StopOn(t, "closing the completed top-up response", response.Body.Close()) }()
	answer := scratchproject.BytesOrStop(t, "reading the completed top-up", outcome.TupleToResult(io.ReadAll(response.Body)))
	assertShopAnswer(t, shopRequestCase{
		name: "the held top-up completed", method: http.MethodPost, path: held.request.URL.Path,
		shopper: "", body: heldShopTopUpBody, headers: []shopRequestHeader{}, bodySchema: outcome.None[shopBodySchema](),
		want: shopAnswer{status: http.StatusAccepted, allow: "", body: `{"_tag":"TopUpQueued","reference":"CAD"}`},
	}, shopAnswer{status: response.StatusCode, allow: response.Header.Get("Allow"), body: string(answer)})
}

func readShopHTTPResponse(t *testing.T, reader *bufio.Reader, request *http.Request) *http.Response {
	t.Helper()
	return outcome.Fold(outcome.TupleToResult(http.ReadResponse(reader, request)), func(response *http.Response) *http.Response { return response },
		func(cause error) *http.Response {
			t.Fatalf("reading the shop's HTTP response: %v", cause)
			return &http.Response{}
		})
}

func awaitShopListenerRefusal(t *testing.T, proofContext context.Context, address string) {
	t.Helper()
	dialer := &net.Dialer{}
	refused := false
	for !refused {
		outcome.Fold(outcome.TupleToResult(dialer.DialContext(proofContext, "tcp", address)), func(connection net.Conn) struct{} {
			scratchproject.StopOn(t, "closing a shutdown probe connection", connection.Close())
			return struct{}{}
		}, func(cause error) struct{} {
			switch {
			case errors.Is(cause, syscall.ECONNREFUSED):
				refused = true
			case errors.Is(cause, syscall.ECONNRESET):
				// A handshake overlapping the listener's close can reset. The proof
				// still requires a later connection to be refused.
			default:
				t.Fatalf("waiting for the listener to refuse a fresh connection: %v", cause)
			}
			return struct{}{}
		})
	}
}
