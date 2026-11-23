package nocontract_test

import (
	"bufio"
	"net/http"
	"strings"
	"testing"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/outcome"
)

func TestHeadersKeepWireOrderAndDistinguishEmptyFromMissing(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		headers string
		want    nocontract.Reading[string]
	}{
		{name: "mixed case field lines retain their order", headers: "X-Text: first\r\nx-TEXT: second\r\n", want: nocontract.Read[string]{Value: "first, second"}},
		{name: "an empty field line is present text", headers: "X-Text:\r\n", want: nocontract.Read[string]{Value: ""}},
		{name: "a missing header has its canonical name", headers: "X-Text-Elsewhere: first\r\n", want: nocontract.RequestRefused{Part: nocontract.RequestRefusedPartHeader, Name: "x-text", Reason: "the header is missing"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := "GET /headers HTTP/1.1\r\nHost: example.test\r\n" + test.headers + "\r\n"
			outcome.Fold(outcome.TupleToResult(http.ReadRequest(bufio.NewReader(strings.NewReader(wire)))), func(request *http.Request) struct{} {
				if got := nocontract.Textual[string](nocontract.Header(request, "X-Text")); got != test.want {
					t.Errorf("header input = %#v; want %#v", got, test.want)
				}
				return struct{}{}
			}, func(cause error) struct{} {
				t.Fatalf("reading the HTTP request: %v", cause)
				return struct{}{}
			})
		})
	}
}
