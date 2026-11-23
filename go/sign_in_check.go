package nocontract

import (
	"context"
	"net/http"
)

// CheckSignIn reads the credential header and returns the application's typed guard decision.
// An absent or empty header calls missing; a nonempty credential calls verify once with the
// request's context and the unchanged credential.
func CheckSignIn[Refusal any](request *http.Request, header string, missing func() Refusal, verify func(context.Context, string) Decision[Refusal]) Decision[Refusal] {
	credential := request.Header.Get(header)
	if credential == "" {
		return Refused[Refusal]{Refusal: missing()}
	}
	return verify(request.Context(), credential)
}
