package running_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CheckPickerUpper/outcome"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

type shopServerAddress struct {
	url    string
	client *http.Client
}

type shopBackend struct {
	language string
	command  string
	prepare  func(*testing.T, shopFixture) string
	open     func(*testing.T, string) shopServerAddress
}

// Every backend serves the same routes, exact decimal values and body shapes, and publishes the
// same operations and schemas. Builds happen once per backend; every request uses a real listener.
func TestShopBackendsConformToTheSameHTTPContract(t *testing.T) {
	checkout := scratchproject.PathOrStop(t, "locating the checkout", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	expected := readShopContract(t, filepath.Join(checkout, "testdata", "go", "shop", "shop_api.openapi.json"))
	requests := shopRequests()
	for _, backend := range []shopBackend{
		{language: "go", command: "check", prepare: useAuthoredGoShop, open: openGoShop},
		{language: "odin", command: "generate", prepare: copyNativeShop, open: openNativeShop},
		{language: "gleam", command: "generate", prepare: copyNativeShop, open: openNativeShop},
	} {
		t.Run(backend.language, func(t *testing.T) {
			fixture := readShopFixture(t, filepath.Join(checkout, "testdata", backend.language, "shop"))
			project := backend.prepare(t, fixture)
			generated := running.Run(running.Request{Arguments: []string{backend.command, "-project", project}, Context: t.Context()})
			if generated.ExitCode() != 0 {
				t.Fatalf("%s %s exited %d:\n%s", backend.command, project, generated.ExitCode(), strings.Join(generated.Lines(), "\n"))
			}
			readShopProjectContract(t, project, func(contract shopContract) {
				assertShopContract(t, expected, contract)
				served := backend.open(t, project)
				for _, request := range requests {
					t.Run(request.name, func(t *testing.T) {
						answer := sendShopRequest(t, served, request)
						assertShopAnswer(t, request, answer)
						assertShopBodySchema(t, contract, request)
						assertShopResponseSchema(t, contract, request, answer)
					})
				}
			})
		})
	}
}

func openGoShop(t *testing.T, project string) shopServerAddress {
	t.Helper()
	directory := filepath.Dir(project)
	binary := filepath.Join(t.TempDir(), "shop")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/shop")
	build.Dir = directory
	started := time.Now()
	compiled := scratchproject.BytesOrStop(t, "building the Go shop", outcome.TupleToResult(build.CombinedOutput()))
	t.Logf("Go shop compilation: %s\n%s", time.Since(started), compiled)
	processContext, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
	t.Cleanup(cancel)
	command := exec.CommandContext(processContext, binary)
	command.Dir = directory
	command.Env = append(os.Environ(), "SHOP_ADDRESS=127.0.0.1:0")
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	return outcome.Fold(outcome.TupleToResult(command.StdoutPipe()), func(stdout io.ReadCloser) shopServerAddress {
		scratchproject.StopOn(t, "starting the Go shop", command.Start())
		t.Cleanup(func() {
			cancel()
			outcome.ForEachOption(outcome.EmptyableToOption(command.Wait()), func(cause error) {
				exited := &exec.ExitError{ProcessState: command.ProcessState, Stderr: []byte{}}
				if !errors.As(cause, &exited) {
					t.Errorf("stopping the Go shop: %v\nstderr:\n%s", cause, stderr)
				}
			})
			if t.Failed() {
				t.Logf("Go shop stderr:\n%s", stderr)
			}
		})
		address := scratchproject.PathOrStop(t, "reading the Go shop's listening address", outcome.TupleToResult(bufio.NewReader(stdout).ReadString('\n')))
		client := *http.DefaultClient
		client.Timeout = 10 * time.Second
		return shopServerAddress{url: "http://" + strings.TrimSpace(address), client: &client}
	}, func(cause error) shopServerAddress {
		t.Fatalf("opening Go shop stdout: %v", cause)
		return shopServerAddress{url: "", client: http.DefaultClient}
	})
}

func openNativeShop(t *testing.T, project string) shopServerAddress {
	t.Helper()
	relocated := filepath.Join(t.TempDir(), "shop")
	scratchproject.StopOn(t, "moving the generated shop to "+relocated, os.Rename(filepath.Dir(project), relocated))
	project = filepath.Join(relocated, filepath.Base(project))
	processContext, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
	t.Cleanup(cancel)
	return outcome.Fold(running.OpenNativeServer(processContext, project, []nocontract.Layer{}), func(opened running.NativeServer) shopServerAddress {
		t.Cleanup(func() { scratchproject.StopOn(t, "closing the native shop from "+project, opened.Close()) })
		served := httptest.NewServer(opened.Handler)
		t.Cleanup(served.Close)
		return shopServerAddress{url: served.URL, client: served.Client()}
	}, func(cause error) shopServerAddress {
		t.Fatalf("opening the shop from %s: %v", project, cause)
		return shopServerAddress{url: "", client: http.DefaultClient}
	})
}

func sendShopRequest(t *testing.T, served shopServerAddress, request shopRequestCase) shopAnswer {
	t.Helper()
	failed := func(step string) func(error) shopAnswer {
		return func(cause error) shopAnswer {
			t.Fatalf("%s %s %s: %v", step, request.method, request.path, cause)
			return shopAnswer{status: 0, allow: "", body: cause.Error()}
		}
	}
	return outcome.Fold(outcome.TupleToResult(http.NewRequestWithContext(t.Context(), request.method, served.url+request.path, strings.NewReader(request.body))),
		func(built *http.Request) shopAnswer {
			built.Header.Set("X-Shopper", request.shopper)
			built.Header.Set("Content-Type", "application/json")
			return outcome.Fold(outcome.TupleToResult(served.client.Do(built)), func(response *http.Response) shopAnswer {
				defer func() { scratchproject.StopOn(t, "closing the response body", response.Body.Close()) }()
				body := scratchproject.BytesOrStop(t, "reading the response body", outcome.TupleToResult(io.ReadAll(response.Body)))
				return shopAnswer{status: response.StatusCode, allow: response.Header.Get("Allow"), body: string(body)}
			}, failed("sending"))
		}, failed("building"))
}

func assertShopAnswer(t *testing.T, request shopRequestCase, answer shopAnswer) {
	t.Helper()
	if answer.status != request.want.status || answer.allow != request.want.allow {
		t.Errorf("%s %s status %d, Allow %q; want %d, Allow %q; body: %s", request.method, request.path, answer.status, answer.allow,
			request.want.status, request.want.allow, answer.body)
	}
	if request.want.body == "" {
		if answer.body != "" {
			t.Errorf("%s %s body %q; want empty", request.method, request.path, answer.body)
		}
		return
	}
	if got, want := canonicalShopJSON(t, answer.body), canonicalShopJSON(t, request.want.body); got != want {
		t.Errorf("%s %s body:\n%s\nwant:\n%s", request.method, request.path, answer.body, request.want.body)
	}
}
