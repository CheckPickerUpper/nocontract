package running_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
			holdRequests := outcome.Fold(outcome.TupleToResult(net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0, Zone: ""})),
				func(listener *net.TCPListener) *net.TCPListener { return listener }, func(cause error) *net.TCPListener {
					t.Fatalf("opening the request hold listener: %v", cause)
					return &net.TCPListener{}
				})
			defer func() { scratchproject.StopOn(t, "closing the request hold listener", holdRequests.Close()) }()
			t.Setenv("SHOP_HOLD_REQUEST_ADDRESS", holdRequests.Addr().String())
			assertShopRequestDrainsOnShutdown(t, backend.open(t, project), holdRequests)
		})
	}
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
	const body = `{"amount":{"cents":"500","currency":"CAD"}}`
	request := outcome.Fold(outcome.TupleToResult(http.NewRequestWithContext(proofContext, http.MethodPost, served.url+"/shoppers/7/top-ups", strings.NewReader(body))),
		func(request *http.Request) *http.Request { return request }, func(cause error) *http.Request {
			t.Fatalf("building the held top-up request: %v", cause)
			return &http.Request{}
		})
	defer func() { scratchproject.StopOn(t, "closing the held request body", request.Body.Close()) }()
	dialer := &net.Dialer{}
	connection := outcome.Fold(outcome.TupleToResult(dialer.DialContext(proofContext, "tcp", request.URL.Host)),
		func(connection net.Conn) net.Conn { return connection }, func(cause error) net.Conn {
			t.Fatalf("opening the held top-up connection: %v", cause)
			return &net.TCPConn{}
		})
	defer func() { scratchproject.StopOn(t, "closing the held top-up connection", connection.Close()) }()
	deadline, _ := proofContext.Deadline()
	scratchproject.StopOn(t, "bounding the held top-up connection", connection.SetDeadline(deadline))
	scratchproject.StopOn(t, "bounding application entry", holdRequests.SetDeadline(deadline))
	outcome.Fold(outcome.TupleToResult(fmt.Fprintf(connection,
		"POST /shoppers/7/top-ups HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		request.URL.Host, len(body), body)), func(int) struct{} { return struct{}{} }, func(cause error) struct{} {
		t.Fatalf("sending the held top-up request: %v", cause)
		return struct{}{}
	})
	held := outcome.Fold(outcome.TupleToResult(holdRequests.AcceptTCP()), func(connection *net.TCPConn) *net.TCPConn { return connection },
		func(cause error) *net.TCPConn {
			t.Fatalf("waiting for the outer application layer to hold the request: %v", cause)
			return &net.TCPConn{}
		})
	defer func() { scratchproject.StopOn(t, "closing the held request's control connection", held.Close()) }()
	scratchproject.StopOn(t, "bounding the held request's release", held.SetDeadline(deadline))

	finished := make(chan []string, 1)
	stopping.Go(func() error { finished <- served.finish(); return nil })
	awaitShopListenerRefusal(t, proofContext, request.URL.Host)
	select {
	case lines := <-finished:
		t.Fatalf("the shop exited while its top-up was still held by the outer layer:\n%s", strings.Join(lines, "\n"))
	default:
	}
	release := [1]byte{1}
	outcome.Fold(outcome.TupleToResult(held.Write(release[:])), func(int) struct{} { return struct{}{} }, func(cause error) struct{} {
		t.Fatalf("releasing the top-up after shutdown began: %v", cause)
		return struct{}{}
	})
	confirmed := [1]byte{}
	outcome.Fold(outcome.TupleToResult(io.ReadFull(held, confirmed[:])), func(int) struct{} { return struct{}{} }, func(cause error) struct{} {
		t.Fatalf("confirming that the application received the release byte: %v", cause)
		return struct{}{}
	})
	if confirmed != release {
		t.Fatalf("the application confirmed release with %v; want %v", confirmed, release)
	}
	response := readShopHTTPResponse(t, bufio.NewReader(connection), request)
	defer func() { scratchproject.StopOn(t, "closing the completed top-up response", response.Body.Close()) }()
	answer := scratchproject.BytesOrStop(t, "reading the completed top-up", outcome.TupleToResult(io.ReadAll(response.Body)))
	assertShopAnswer(t, shopRequestCase{
		name: "a top-up completed during shutdown", method: http.MethodPost, path: request.URL.Path,
		shopper: "", body: body, headers: []shopRequestHeader{}, bodySchema: outcome.None[shopBodySchema](),
		want: shopAnswer{status: http.StatusAccepted, allow: "", body: `{"_tag":"TopUpQueued","reference":"CAD"}`},
	}, shopAnswer{status: response.StatusCode, allow: response.Header.Get("Allow"), body: string(answer)})
	select {
	case <-finished:
	case <-proofContext.Done():
		t.Fatalf("waiting for the shop to finish its shutdown: %v", proofContext.Err())
	}
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
