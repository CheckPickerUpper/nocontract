package writing_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/writing"
)

func TestSharedFailuresKeepNativeIntegerFieldsAndDecimalTextMarshalers(t *testing.T) {
	t.Parallel()
	widths := []struct {
		field  string
		goName string
		goType string
		width  description.IntegerWidth
	}{
		{field: "signed_8", goName: "Signed8", goType: "int8", width: description.Signed8{}},
		{field: "signed_16", goName: "Signed16", goType: "int16", width: description.Signed16{}},
		{field: "signed_32", goName: "Signed32", goType: "int32", width: description.Signed32{}},
		{field: "signed_64", goName: "Signed64", goType: "int64", width: description.Signed64{}},
		{field: "unsigned_8", goName: "Unsigned8", goType: "uint8", width: description.Unsigned8{}},
		{field: "unsigned_16", goName: "Unsigned16", goType: "uint16", width: description.Unsigned16{}},
		{field: "unsigned_32", goName: "Unsigned32", goType: "uint32", width: description.Unsigned32{}},
		{field: "unsigned_64", goName: "Unsigned64", goType: "uint64", width: description.Unsigned64{}},
		{field: "another_signed_64", goName: "AnotherSigned64", goType: "int64", width: description.Signed64{}},
	}
	fields := []description.Field{}
	for _, each := range widths {
		switch parsed := description.ParseFieldName(each.field).(type) {
		case description.FieldNameRead:
			fields = append(fields, description.Field{Name: parsed.Name, Schema: description.Integer{Width: each.width}})
		case description.FieldNameRefused, nil:
			t.Fatalf("fixture field %q was refused", each.field)
		}
	}
	failure := description.SharedFailure{
		Name: "IntegerFailure", Status: description.SharedFailures()[0].Status, Code: "NT-9000", Fields: fields,
		Answers: description.AnsweredByEveryRoute{}, Doc: "IntegerFailure carries each whole-number width.",
		Message: []description.MessagePart{description.MessageText{Text: "The whole-number request was refused."}},
	}
	outcome.Fold(writing.RenderRuntime([]description.SharedFailure{failure}), func(source []byte) struct{} {
		text := strings.Join(strings.Fields(string(source)), " ")
		for _, each := range widths {
			declaration := fmt.Sprintf("%s %s `json:%q`", each.goName, each.goType, each.field)
			if !strings.Contains(text, declaration) {
				t.Errorf("integer field declaration %s is missing from:\n%s", declaration, source)
			}
		}
		marshalers := "}, member, AnswerMarshalers{SignedText[int64](), UnsignedText[uint64]()})"
		if !strings.Contains(text, marshalers) {
			t.Errorf("integer answer does not use each decimal-text marshaler once:\n%s", source)
		}
		return struct{}{}
	}, func(cause error) struct{} {
		t.Fatalf("rendering a checked whole-number failure: %v", cause)
		return struct{}{}
	})
}
