package running_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

type shopServerAddress struct {
	url    string
	client *http.Client
	finish func() []string
}

type shopBackend struct {
	language string
	prepare  func(*testing.T, shopFixture) string
	open     func(*testing.T, string) shopServerAddress
}

// Every backend serves the same routes, exact decimal values and body shapes, and publishes the
// same operations and schemas. Builds happen once per backend; every request uses a real listener.
func TestShopBackendsConformToTheSameHTTPContract(t *testing.T) {
	checkout := scratchproject.PathOrStop(t, "locating the checkout", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	expectedFiles := readShopGeneratedFiles(t, filepath.Join(checkout, "testdata", "go", "shop", "nocontract.json"))
	requests := shopRequests()
	contract := readShopContract(t, expectedFiles.document)
	assertShopOperationIDs(t, contract)
	for _, request := range requests {
		assertShopBodySchema(t, contract, request)
	}
	client := prepareShopClient(t, checkout, expectedFiles.client)
	for _, backend := range shopBackends() {
		t.Run(backend.language, func(t *testing.T) {
			served := openCheckedShop(t, checkout, backend, expectedFiles)
			observed := []shopAnsweredRequest{}
			for _, request := range requests {
				t.Run(request.name, func(t *testing.T) {
					answer := sendShopRequest(t, served, request)
					assertShopAnswer(t, request, answer)
					assertShopResponseSchema(t, contract, request, answer)
					observed = append(observed, shopAnsweredRequest{request: request, answer: answer})
				})
			}
			assertShopClientAnswers(t, client, served)
			assertShopObservations(t, contract, observed, served.finish())
		})
	}
}

func shopBackends() []shopBackend {
	return []shopBackend{
		{language: "go", prepare: copyGoShop, open: openGoShop},
		{language: "odin", prepare: copyShop, open: openOdinShop},
		{language: "gleam", prepare: copyGleamShop, open: openGleamShop},
	}
}

func openCheckedShop(t *testing.T, checkout string, backend shopBackend, expectedFiles shopGeneratedFiles) shopServerAddress {
	t.Helper()
	fixture := readShopFixture(t, filepath.Join(checkout, "testdata", backend.language, "shop"))
	project := backend.prepare(t, fixture)
	for _, command := range []string{"generate", "check"} {
		result := running.Run(running.Request{Arguments: []string{command, "-project", project}, Context: t.Context()})
		if result.ExitCode() != 0 {
			t.Fatalf("%s %s exited %d:\n%s", command, project, result.ExitCode(), strings.Join(result.Lines(), "\n"))
		}
	}
	generated := readShopGeneratedFiles(t, project)
	assertShopGeneratedFile(t, shopFileComparison{artifact: "TypeScript", expected: expectedFiles.client, actual: generated.client})
	assertShopGeneratedFile(t, shopFileComparison{artifact: "OpenAPI", expected: expectedFiles.document, actual: generated.document})
	if t.Failed() {
		t.FailNow()
	}
	return backend.open(t, project)
}

func openGoShop(t *testing.T, project string) shopServerAddress {
	t.Helper()
	directory := filepath.Dir(project)
	binary := filepath.Join(t.TempDir(), "shop")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/shop")
	build.Dir = directory
	runShopCommand(t, build)
	processContext, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
	t.Cleanup(cancel)
	command := exec.CommandContext(processContext, binary)
	command.Dir = directory
	command.Env = append(os.Environ(), "SHOP_ADDRESS=127.0.0.1:0")
	return openShopProcess(t, command, shopProcessShutdown{
		stop: func() {
			outcome.ForEachOption(outcome.EmptyableToOption(command.Process.Signal(os.Interrupt)), func(cause error) {
				if !errors.Is(cause, os.ErrProcessDone) {
					t.Errorf("interrupting the Go shop: %v", cause)
				}
			})
		},
		verify: func(cause error) { t.Errorf("waiting for the Go shop's interrupt shutdown: %v", cause) },
	})
}

func openGleamShop(t *testing.T, project string) shopServerAddress {
	t.Helper()
	lifetime, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
	t.Cleanup(cancel)
	command := exec.CommandContext(lifetime, "gleam", "run", "--no-print-progress", "-m", "shop_server")
	command.Dir = filepath.Dir(project)
	command.WaitDelay = 2 * time.Second
	configureGleamShopProcess(command)
	return openShopProcess(t, command, shopProcessShutdown{
		stop: cancel,
		verify: func(cause error) {
			outcome.ForEachOption(outcome.EmptyableToOption(cause), func(cause error) {
				if errors.Is(cause, context.Canceled) && command.ProcessState.Success() {
					return
				}
				if _, exited := errors.AsType[*exec.ExitError](cause); !exited {
					t.Errorf("stopping the Gleam shop: %v", cause)
				}
			})
		},
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
			for _, header := range request.headers {
				built.Header[header.name] = []string{header.value}
			}
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
