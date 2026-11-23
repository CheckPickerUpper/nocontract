package writing_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/writing"
)

func TestSharedLiteralNamesKeepHyphenatedAndUnderscoredWireValues(t *testing.T) {
	t.Parallel()
	literals := []struct {
		wire   string
		suffix string
	}{
		{wire: "content-length", suffix: "ContentLength"},
		{wire: "received_bytes", suffix: "ReceivedBytes"},
	}
	values := []string{}
	for _, literal := range literals {
		values = append(values, literal.wire)
	}
	switch parsed := description.ParseFieldName("source").(type) {
	case description.FieldNameRead:
		failure := description.SharedFailure{
			Name: "LiteralFailure", Status: description.SharedFailures()[0].Status, Code: "NT-9000",
			Fields:  []description.Field{{Name: parsed.Name, Schema: description.Literals{Values: values}}},
			Answers: description.AnsweredByEveryRoute{}, Doc: "LiteralFailure identifies the request's source.",
			Message: []description.MessagePart{description.MessageText{Text: "The request was refused."}},
		}
		outcome.Fold(writing.RenderRuntime([]description.SharedFailure{failure}), func(source []byte) struct{} {
			text := strings.Join(strings.Fields(string(source)), " ")
			for _, literal := range literals {
				declaration := fmt.Sprintf("LiteralFailureSource%s LiteralFailureSource = %q", literal.suffix, literal.wire)
				if !strings.Contains(text, declaration) {
					t.Errorf("literal declaration %s is missing from:\n%s", declaration, source)
				}
			}
			return struct{}{}
		}, func(cause error) struct{} {
			t.Fatalf("rendering a shared literal failure: %v", cause)
			return struct{}{}
		})
	case description.FieldNameRefused, nil:
		t.Fatal("fixture field source was refused")
	}
}
