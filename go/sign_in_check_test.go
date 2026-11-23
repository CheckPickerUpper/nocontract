package nocontract_test

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/http/httptest"
	"testing"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

//sumtype:decl
type signInRefusal interface {
	isSignInRefusal()
}

type credentialMissing struct {
	header string
}

type credentialRejected struct {
	scope string
}

func (credentialMissing) isSignInRefusal()  {}
func (credentialRejected) isSignInRefusal() {}

func TestSignInUsesTheChosenHeaderAndReturnsTheApplicationsDecision(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		name          string
		headers       http.Header
		want          nocontract.Decision[signInRefusal]
		missingCalls  int
		verifiedCalls int
	}{
		{
			name: "absent credential", headers: http.Header{"X-Shopper": {"shopper-7"}},
			want:         nocontract.Refused[signInRefusal]{Refusal: credentialMissing{header: "x-session"}},
			missingCalls: 1, verifiedCalls: 0,
		},
		{
			name: "empty credential", headers: http.Header{"X-Session": {""}},
			want:         nocontract.Refused[signInRefusal]{Refusal: credentialMissing{header: "x-session"}},
			missingCalls: 1, verifiedCalls: 0,
		},
		{
			name: "verified credential", headers: http.Header{"X-Session": {"shopper-7"}},
			want: nocontract.Admitted[signInRefusal]{}, missingCalls: 0, verifiedCalls: 1,
		},
		{
			name: "rejected credential", headers: http.Header{"X-Session": {"shopper-8"}},
			want:         nocontract.Refused[signInRefusal]{Refusal: credentialRejected{scope: "read-balance"}},
			missingCalls: 0, verifiedCalls: 1,
		},
		{
			name: "credential preserved", headers: http.Header{"X-Session": {" shopper-7 "}},
			want:         nocontract.Refused[signInRefusal]{Refusal: credentialRejected{scope: "read-balance"}},
			missingCalls: 0, verifiedCalls: 1,
		},
	} {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/balance", http.NoBody)
			request.Header = each.headers
			missingCalls, verifiedCalls := 0, 0
			missing := func() signInRefusal {
				missingCalls++
				return credentialMissing{header: "x-session"}
			}
			verify := func(requestContext context.Context, credential string) nocontract.Decision[signInRefusal] {
				verifiedCalls++
				if requestContext != request.Context() {
					t.Fatal("the application verifier did not receive the request context")
				}
				if subtle.ConstantTimeCompare([]byte(credential), []byte("shopper-7")) == 1 {
					return nocontract.Admitted[signInRefusal]{}
				}
				return nocontract.Refused[signInRefusal]{Refusal: credentialRejected{scope: "read-balance"}}
			}
			if got := nocontract.CheckSignIn(request, "x-session", missing, verify); got != each.want {
				t.Errorf("sign-in decision = %#v; want %#v", got, each.want)
			}
			if missingCalls != each.missingCalls || verifiedCalls != each.verifiedCalls {
				t.Errorf("missing calls = %d, verifier calls = %d; want %d, %d", missingCalls, verifiedCalls, each.missingCalls, each.verifiedCalls)
			}
		})
	}
}
