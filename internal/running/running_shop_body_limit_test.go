package running_test

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/outcome"
)

const shopBodyLimit = 1024
const shopTopUpBody = `{"amount":{"cents":"500","currency":"CAD"}}`

func oversizedShopBodyRequest() shopRequestCase {
	return shopRequestCase{
		name: "Content-Length exceeds the configured body limit", method: http.MethodPost, path: "/shoppers/7/top-ups", shopper: "", body: shopTopUpBody + strings.Repeat(" ", 1536-len(shopTopUpBody)),
		headers: []shopRequestHeader{}, want: shopAnswer{status: 413, allow: "", body: `{"_tag":"RequestBodyTooLarge","code":"NC-1168","limit_bytes":"1024","size_bytes":"1536","size_source":"content-length"}`},
		bodySchema: outcome.Some(shopBodySchema{path: "/shoppers/{shopper_id}/top-ups", accepts: true}),
	}
}

func checkShopBodyLimits(t *testing.T, served shopServerAddress, contract shopContract) []shopAnsweredRequest {
	t.Helper()
	exact := shopTopUpBody + strings.Repeat(" ", shopBodyLimit-len(shopTopUpBody))
	observed := []shopAnsweredRequest{}
	for _, request := range []shopRequestCase{
		{
			name: "a JSON body exactly at the configured limit", method: http.MethodPost, path: "/shoppers/7/top-ups", shopper: "", body: exact,
			headers: []shopRequestHeader{}, want: shopAnswer{status: 202, allow: "", body: `{"_tag":"TopUpQueued","reference":"CAD"}`},
			bodySchema: outcome.Some(shopBodySchema{path: "/shoppers/{shopper_id}/top-ups", accepts: true}),
		},
		oversizedShopBodyRequest(),
	} {
		t.Run(request.name, func(t *testing.T) {
			answer := sendShopRequest(t, served, request)
			assertShopAnswer(t, request, answer)
			assertShopResponseSchema(t, contract, request, answer)
			observed = append(observed, shopAnsweredRequest{request: request, answer: answer})
		})
	}
	chunked := shopRequestCase{
		name: "a chunked body exceeds the configured limit", method: http.MethodPost, path: "/shoppers/7/top-ups", shopper: "", body: exact + strings.Repeat(" ", 512),
		headers: []shopRequestHeader{}, want: shopAnswer{status: 413, allow: "", body: `{"_tag":"RequestBodyTooLarge","code":"NC-1168","limit_bytes":"1024","size_bytes":"1025","size_source":"stream"}`},
		bodySchema: outcome.Some(shopBodySchema{path: "/shoppers/{shopper_id}/top-ups", accepts: true}),
	}
	t.Run(chunked.name, func(t *testing.T) {
		answer := sendShopRequestBody(t, served, chunked, io.NopCloser(strings.NewReader(chunked.body)))
		assertShopAnswer(t, chunked, answer)
		assertShopResponseSchema(t, contract, chunked, answer)
		observed = append(observed, shopAnsweredRequest{request: chunked, answer: answer})
	})
	partial := chunked
	partial.name = "a partial HTTP chunk is refused while later bytes remain unsent"
	t.Run(partial.name, func(t *testing.T) {
		wire := "POST /shoppers/7/top-ups HTTP/1.1\r\nHost: shop\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n1000\r\n" + exact + " "
		answer := sendShopWireRequest(t, served, wire)
		assertShopAnswer(t, partial, answer)
		assertShopResponseSchema(t, contract, partial, answer)
		observed = append(observed, shopAnsweredRequest{request: partial, answer: answer})
	})
	declared := shopRequestCase{
		name: "a large Content-Length is refused before any payload is sent", method: http.MethodPost, path: "/shoppers/7/top-ups", shopper: "", body: "",
		headers: []shopRequestHeader{}, want: shopAnswer{status: 413, allow: "", body: `{"_tag":"RequestBodyTooLarge","code":"NC-1168","limit_bytes":"1024","size_bytes":"9007199254740993","size_source":"content-length"}`},
		bodySchema: outcome.None[shopBodySchema](),
	}
	t.Run(declared.name, func(t *testing.T) {
		wire := "POST /shoppers/7/top-ups HTTP/1.1\r\nHost: shop\r\nContent-Type: application/json\r\nContent-Length: 9007199254740993\r\nConnection: close\r\n\r\n"
		answer := sendShopWireRequest(t, served, wire)
		assertShopAnswer(t, declared, answer)
		assertShopResponseSchema(t, contract, declared, answer)
		observed = append(observed, shopAnsweredRequest{request: declared, answer: answer})
	})
	preflight := shopRequestCase{
		name: "Content-Length is refused before the client sends its payload", method: http.MethodPost, path: "/shoppers/7/top-ups", shopper: "", body: "",
		headers: []shopRequestHeader{}, want: shopAnswer{status: 413, allow: "", body: `{"_tag":"RequestBodyTooLarge","code":"NC-1168","limit_bytes":"1024","size_bytes":"1025","size_source":"content-length"}`},
		bodySchema: outcome.None[shopBodySchema](),
	}
	t.Run(preflight.name, func(t *testing.T) {
		wire := "POST /shoppers/7/top-ups HTTP/1.1\r\nHost: shop\r\nContent-Type: application/json\r\nContent-Length: 1025\r\n\r\n"
		answer := sendShopWireRequest(t, served, wire)
		assertShopAnswer(t, preflight, answer)
		assertShopResponseSchema(t, contract, preflight, answer)
		observed = append(observed, shopAnsweredRequest{request: preflight, answer: answer})
	})
	return observed
}

func sendShopWireRequest(t *testing.T, served shopServerAddress, wire string) shopAnswer {
	t.Helper()
	dialer := net.Dialer{Timeout: 3 * time.Second}
	return outcome.Fold(outcome.TupleToResult(dialer.DialContext(t.Context(), "tcp", strings.TrimPrefix(served.url, "http://"))), func(connection net.Conn) shopAnswer {
		defer func() { scratchproject.StopOn(t, "closing the unfinished request connection", connection.Close()) }()
		scratchproject.StopOn(t, "limiting the unfinished request wait", connection.SetDeadline(time.Now().Add(3*time.Second)))
		outcome.Fold(outcome.TupleToResult(io.WriteString(connection, wire)), func(int) struct{} { return struct{}{} }, func(cause error) struct{} {
			t.Fatalf("sending an unfinished body: %v", cause)
			return struct{}{}
		})
		return outcome.Fold(outcome.TupleToResult(http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})), func(response *http.Response) shopAnswer {
			defer func() { scratchproject.StopOn(t, "closing the unfinished request response", response.Body.Close()) }()
			content := scratchproject.BytesOrStop(t, "reading the refusal before sending the rest of the body", outcome.TupleToResult(io.ReadAll(response.Body)))
			return shopAnswer{status: response.StatusCode, allow: response.Header.Get("Allow"), body: string(content)}
		}, func(cause error) shopAnswer {
			t.Fatalf("waiting for a size refusal while the remaining payload is unsent: %v", cause)
			return shopAnswer{status: 0, allow: "", body: cause.Error()}
		})
	}, func(cause error) shopAnswer {
		t.Fatalf("opening an unfinished body request: %v", cause)
		return shopAnswer{status: 0, allow: "", body: cause.Error()}
	})
}
