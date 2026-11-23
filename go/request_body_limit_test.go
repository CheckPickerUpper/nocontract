package nocontract_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/outcome"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestDeclaredBodyLengthGetsAResponseBeforeThePayloadArrives(t *testing.T) {
	outcome.Fold(nocontract.ParseRequestBodyLimit(8), func(limit nocontract.RequestBodyLimit) struct{} {
		binding := nocontract.BindRoutes([]nocontract.Endpoint{{
			Method: http.MethodPost, Path: []nocontract.PathSegment{nocontract.PathText{Text: "upload"}},
			Answer: func(request *http.Request, limit nocontract.RequestBodyLimit) nocontract.Answer {
				return nocontract.Then(nocontract.Body(request, limit, nocontract.BodyText[string]), func(string) nocontract.Answer {
					return nocontract.Succeeded(nocontract.Success{Status: 200, Tag: "Received"}, struct{}{}, nocontract.AnswerMarshalers{})
				})
			},
		}}, []nocontract.Layer{})
		options := nocontract.WithRequestBodyLimit(nocontract.ListenOn("127.0.0.1:0"), limit)
		return outcome.Fold(nocontract.Listen(t.Context(), options, binding), func(listening nocontract.ListeningServer) struct{} {
			defer func() {
				outcome.Fold(listening.Stop(context.WithoutCancel(t.Context())), func(nocontract.ServerStopped) struct{} { return struct{}{} }, func(cause error) struct{} { t.Error(cause); return struct{}{} })
			}()
			dialer := net.Dialer{Timeout: 3 * time.Second}
			return outcome.Fold(outcome.TupleToResult(dialer.DialContext(t.Context(), "tcp", listening.Address())), func(connection net.Conn) struct{} {
				defer func() {
					outcome.ForEachOption(outcome.EmptyableToOption(connection.Close()), func(cause error) { t.Error(cause) })
				}()
				outcome.ForEachOption(outcome.EmptyableToOption(connection.SetDeadline(time.Now().Add(3*time.Second))), func(cause error) { t.Fatal(cause) })
				headers := "POST /upload HTTP/1.1\r\nHost: test\r\nContent-Length: 9\r\n\r\n"
				outcome.Fold(outcome.TupleToResult(io.WriteString(connection, headers)), func(int) struct{} { return struct{}{} }, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
				return outcome.Fold(outcome.TupleToResult(http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})), func(response *http.Response) struct{} {
					defer func() {
						outcome.ForEachOption(outcome.EmptyableToOption(response.Body.Close()), func(cause error) { t.Error(cause) })
					}()
					if response.StatusCode != http.StatusRequestEntityTooLarge || !response.Close {
						t.Errorf("header-only request returned status %d, close %t; want 413 and a closed connection", response.StatusCode, response.Close)
					}
					return struct{}{}
				}, func(cause error) struct{} {
					t.Fatalf("waiting for a response while all payload bytes are withheld: %v", cause)
					return struct{}{}
				})
			}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
		}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
	}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
}

func TestBodyBoundsAnUnknownLengthRequestWithoutAListener(t *testing.T) {
	outcome.Fold(nocontract.ParseRequestBodyLimit(8), func(limit nocontract.RequestBodyLimit) struct{} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`"123456789"`))
		request.ContentLength = -1
		read := nocontract.Body(request, limit, nocontract.BodyText[string])
		want := nocontract.RequestBodyTooLarge{LimitBytes: 8, SizeBytes: 9, SizeSource: nocontract.RequestBodyTooLargeSizeSourceStream}
		if difference := cmp.Diff(want, read); difference != "" {
			t.Errorf("body reading differs (-want +read):\n%s", difference)
		}
		return struct{}{}
	}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
}

func TestBodyKeepsThePartialPayloadAndNativeReadFailure(t *testing.T) {
	outcome.Fold(nocontract.ParseRequestBodyLimit(8), func(limit nocontract.RequestBodyLimit) struct{} {
		body := io.MultiReader(strings.NewReader(`"abc`), iotest.ErrReader(io.ErrUnexpectedEOF))
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", body)
		read := nocontract.Body(request, limit, nocontract.BodyText[string])
		want := nocontract.RequestBodyReceiveFailed{Limit: 8, Content: []byte(`"abc`), Cause: io.ErrUnexpectedEOF}
		if difference := cmp.Diff(want, read, cmpopts.EquateErrors()); difference != "" {
			t.Errorf("body reading differs (-want +read):\n%s", difference)
		}
		return struct{}{}
	}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
}
