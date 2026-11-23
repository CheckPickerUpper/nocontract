package description_test

import (
	"testing"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/google/go-cmp/cmp"
)

func TestInvalidHeaderNameKeepsTheOffendingByte(t *testing.T) {
	t.Parallel()
	name := "X-\xff-Count"
	parsed, refused := description.ParseHeaderName(name).(description.HeaderNameRefused)
	if !refused {
		t.Fatalf("header name %q was accepted", name)
	}
	want := description.HeaderNameInvalid{Name: name, Character: "\xff", Offset: 2}
	if difference := cmp.Diff(want, parsed.Problem); difference != "" {
		t.Errorf("header name refusal differs (-want +got):\n%s", difference)
	}
}
