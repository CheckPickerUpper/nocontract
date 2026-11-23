package running_test

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/outcome"
)

func TestShopBackendsReturnDeclaredDeadlines(t *testing.T) {
	t.Setenv("SHOP_TEST_REQUEST_DEADLINE", "1")
	checkout := scratchproject.PathOrStop(t, "locating the checkout", outcome.TupleToResult(filepath.Abs(filepath.Join("..", ".."))))
	expectedFiles := readShopGeneratedFiles(t, filepath.Join(checkout, "testdata", "go", "shop", "nocontract.json"))
	contract := readShopContract(t, expectedFiles.document)
	for path, methods := range contract.document.Paths {
		for method, operation := range methods {
			if _, declared := operation.Responses["503"]; !declared {
				t.Errorf("%s %s declares no request expiration response", method, path)
			}
		}
	}
	client := prepareShopClient(t, checkout, expectedFiles.client)
	for _, backend := range shopBackends() {
		t.Run(backend.language, func(t *testing.T) {
			served := openCheckedShop(t, checkout, backend, expectedFiles)
			observed := []shopAnsweredRequest{}
			for _, request := range shopDeadlineRequests() {
				t.Run(request.name, func(t *testing.T) {
					started := time.Now()
					answer := sendShopRequest(t, served, request)
					elapsed := time.Since(started)
					assertShopAnswer(t, request, answer)
					assertShopResponseSchema(t, contract, request, answer)
					if request.want.status == http.StatusServiceUnavailable && elapsed >= 2*time.Second {
						t.Errorf("the answer arrived after %s, after the fixture's outstanding work could finish", elapsed)
					}
					observed = append(observed, shopAnsweredRequest{request: request, answer: answer})
					t.Logf("GET %s = %d %s after %s", request.path, answer.status, answer.body, elapsed)
				})
			}
			command := exec.CommandContext(t.Context(), "bun", "run", "--no-install", "src/request_deadline.ts")
			command.Dir = client
			command.Env = append(os.Environ(), "SHOP_URL="+served.url)
			decoded := runShopCommand(t, command)
			t.Logf("Decoded request deadline: %s", strings.TrimSpace(string(decoded)))
			request := shopDeadlineRequests()[1]
			answer := shopAnswer{status: http.StatusServiceUnavailable, allow: "", body: strings.TrimSpace(string(decoded))}
			assertShopAnswer(t, request, answer)
			observed = append(observed, shopAnsweredRequest{request: request, answer: answer})
			lines := served.finish()
			assertShopDeadlineLogs(t, contract, observed, lines)
			if backend.language == "odin" {
				const cancellation = "The delayed balance observed cancellation after 50 milliseconds"
				if !strings.Contains(strings.Join(lines, "\n"), cancellation) {
					t.Errorf("the native handler did not observe its deadline cancellation:\n%s", strings.Join(lines, "\n"))
				}
				t.Log(cancellation)
			}
		})
	}
}

func shopDeadlineRequests() []shopRequestCase {
	return []shopRequestCase{
		{name: "fast work finishes", method: http.MethodGet, path: "/shoppers/7/delayed-balance?currency=CAD&delay_milliseconds=0", shopper: "", headers: []shopRequestHeader{}, body: "", want: shopAnswer{status: 200, allow: "", body: `{"_tag":"BalanceRead","balance":{"cents":"1250","currency":"CAD"},"overdraft":null,"changed":"2026-11-14T12:00:00Z","note":"first top-up"}`}, bodySchema: outcome.None[shopBodySchema]()},
		{name: "slow work exceeds its deadline", method: http.MethodGet, path: "/shoppers/7/delayed-balance?currency=CAD&delay_milliseconds=2000", shopper: "", headers: []shopRequestHeader{}, body: "", want: shopAnswer{status: 503, allow: "", body: `{"_tag":"RequestDeadlineExceeded","code":"NC-1170","route":"/shoppers/{shopper_id}/delayed-balance","timeout_milliseconds":50}`}, bodySchema: outcome.None[shopBodySchema]()},
	}
}

func assertShopDeadlineLogs(t *testing.T, contract shopContract, requests []shopAnsweredRequest, lines []string) {
	t.Helper()
	logged := []shopObservation{}
	for _, entry := range readShopObservations(t, lines) {
		if entry.Layer == "" && entry.Phase == "" {
			logged = append(logged, entry)
		}
	}
	if len(logged) != len(requests) {
		t.Fatalf("got %d final request logs for %d deadline HTTP answers:\n%s", len(logged), len(requests), strings.Join(lines, "\n"))
	}
	for index, request := range requests {
		assertShopLoggedDuration(t, index, logged[index])
		assertShopLogMatchesAnswer(t, contract, request, logged[index])
	}
}
