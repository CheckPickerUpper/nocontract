package bindings

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/outcome"
)

func TestSharedFailureIntegersUseTheDeclaredWidthsNativeEncoder(t *testing.T) {
	t.Parallel()
	widths := []struct {
		field   string
		width   description.IntegerWidth
		encoder string
	}{
		{field: "signed_8", width: description.Signed8{}, encoder: "json.int(value)"},
		{field: "signed_16", width: description.Signed16{}, encoder: "json.int(value)"},
		{field: "signed_32", width: description.Signed32{}, encoder: "json.int(value)"},
		{field: "signed_64", width: description.Signed64{}, encoder: "value |> int.to_string |> json.string"},
		{field: "unsigned_8", width: description.Unsigned8{}, encoder: "json.int(value)"},
		{field: "unsigned_16", width: description.Unsigned16{}, encoder: "json.int(value)"},
		{field: "unsigned_32", width: description.Unsigned32{}, encoder: "json.int(value)"},
		{field: "unsigned_64", width: description.Unsigned64{}, encoder: "value |> int.to_string |> json.string"},
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
	project := Project{Directory: "", Server: "src/nocontract", Routes: []Route{}, Guards: map[string]Guard{}, Records: map[compiler.Identity]Record{}}
	api := description.API{Name: description.APIName{}, Groups: []description.Group{}, Members: []description.Member{}, Structs: []description.NamedStruct{}}
	outcome.Fold(generateServer(project, api, append(description.SharedFailures(), failure)), func(files []native.File) struct{} {
		if len(files) != 1 || files[0].Path != "src/nocontract/server.gleam" {
			t.Fatalf("generated files are %+v; want the native server", files)
		}
		source := string(files[0].Contents)
		for _, each := range widths {
			argument := each.field + " " + each.field + ": Int"
			if !strings.Contains(source, argument) {
				t.Errorf("native integer argument %q is missing", argument)
			}
			field := regexp.MustCompile(fmt.Sprintf(`#\("%s", (\w+)\(%s\)\)`, each.field, each.field)).FindStringSubmatch(source)
			if len(field) != 2 {
				t.Errorf("integer field %q does not call its encoder", each.field)
				continue
			}
			encoder := "fn " + field[1] + "(value: Int) -> json.Json {\n" + each.encoder + "\n}"
			if !strings.Contains(source, encoder) {
				t.Errorf("integer field %q has no matching encoder definition:\n%s", each.field, encoder)
			}
		}
		for _, required := range []string{"import gleam/int as int", `Answer(response:, code: option.Some("NT-9000"))`} {
			if !strings.Contains(source, required) {
				t.Errorf("native server is missing %q", required)
			}
		}
		return struct{}{}
	}, func(cause error) struct{} {
		t.Fatalf("generating a checked whole-number failure: %v", cause)
		return struct{}{}
	})
}
