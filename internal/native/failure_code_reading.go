package native

import (
	"fmt"
	"strings"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/CheckPickerUpper/ErGo/codes"
	"github.com/CheckPickerUpper/outcome"
)

// FailureCodePrefixInvalid preserves the declared code and the rejected prefix.
type FailureCodePrefixInvalid struct {
	Written string
	Problem codes.InvalidPrefix
}

// Error identifies the prefix rule the declared code broke.
func (failure FailureCodePrefixInvalid) Error() string {
	return fmt.Sprintf("invalid permanent code %q: prefix %q has problem %+v", failure.Written, failure.Problem.Text, failure.Problem.Fault)
}

// FailureCodeInvalid preserves the permanent-code parser's refusal.
type FailureCodeInvalid struct{ Problem codes.InvalidCode }

// Error describes why the permanent code was refused.
func (failure FailureCodeInvalid) Error() string {
	return fmt.Sprintf("invalid permanent code: %+v", failure.Problem)
}

type failureCodeUnread struct{ written string }

func (failure failureCodeUnread) Error() string {
	return fmt.Sprintf("the permanent code parser returned no result for %q", failure.written)
}

// ParseFailureCode checks a source declaration's permanent error code with ErGo's parser.
func ParseFailureCode(written string) outcome.Result[ergo.Code] {
	prefix, _, _ := strings.Cut(written, "-")
	switch parsed := codes.ParsePrefix(prefix).(type) {
	case codes.ParsedPrefix:
		switch code := codes.Parse(codes.ParseRequest{Text: written, Prefix: parsed.Prefix}).(type) {
		case codes.ParsedCode:
			return outcome.Ok(code.Code)
		case codes.InvalidCode:
			return outcome.Err[ergo.Code](FailureCodeInvalid{Problem: code})
		case nil:
		}
	case codes.InvalidPrefix:
		return outcome.Err[ergo.Code](FailureCodePrefixInvalid{Written: written, Problem: parsed})
	case nil:
	}
	return outcome.Err[ergo.Code](failureCodeUnread{written: written})
}
