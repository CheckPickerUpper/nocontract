package running_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/internal/running"
)

// project is the absolute path of the project file of the example project named.
func project(t *testing.T, name string) string {
	t.Helper()
	relative := filepath.Join("..", "..", "testdata", "go", name, "nocontract.json")
	return outcome.Fold(outcome.TupleToResult(filepath.Abs(relative)), func(path string) string { return path }, func(cause error) string {
		t.Fatalf("resolving %s: %v", relative, cause)
		return relative
	})
}

// check is the check command run on the example project named.
func check(t *testing.T, name string) running.Outcome {
	t.Helper()
	return running.Run(running.Request{Arguments: []string{"check", "-project", project(t, name)}, Context: t.Context()})
}

// The example shop's committed Go server file and Effect API are what generate writes from its
// routes, so a change to the reader or either writer that changes a file fails here until the
// files are generated again, the shop builds, and its TypeScript is checked.
func TestTheShopsCommittedFilesAreWhatGenerateWrites(t *testing.T) {
	t.Parallel()
	outcome := check(t, "shop")
	if _, isCurrent := outcome.(running.FilesCurrent); !isCurrent {
		t.Fatalf("check answered %v; want the shop's files current", outcome.Lines())
	}
}

// TestTheRuntimesCommittedFileIsWhatGenerateWrites holds the runtime's answers to its shared
// failures to the statuses their directives declare.
func TestTheRuntimesCommittedFileIsWhatGenerateWrites(t *testing.T) {
	t.Parallel()
	outcome := running.RunRuntime(running.Request{Arguments: []string{"check"}, Context: t.Context()})
	if _, isCurrent := outcome.(running.FilesCurrent); !isCurrent {
		t.Fatalf("check answered %v; want the runtime's file current", outcome.Lines())
	}
}

func TestEachWrongDeclarationIsRefusedWithItsCode(t *testing.T) {
	t.Parallel()
	checked := check(t, "refused")
	refused, isRefused := checked.(running.DeclarationsRefused)
	if !isRefused {
		t.Fatalf("check answered %#v; want the refused project's declarations refused", checked)
	}
	codes := []string{}
	for _, refusal := range refused.Refusals {
		codes = append(codes, refusal.Problem.ErrorCode().String())
		// The project names its API Schema, which the client imports, so the name is refused
		// where the project file gives it.
		if refusal.Position.File == project(t, "refused") && (refusal.Position.Line != 5 || refusal.Position.Column != 10) {
			t.Errorf("the API's name was refused at %s; want line 5, column 10", refusal.Position)
		}
	}
	slices.Sort(codes)
	want := []string{
		"NC-1005", "NC-1012", "NC-1013", "NC-1014", "NC-1015", "NC-1016", "NC-1017", "NC-1018",
		"NC-1022", "NC-1025", "NC-1027", "NC-1029", "NC-1033", "NC-1046", "NC-1048", "NC-1049", "NC-1050",
		"NC-1051", "NC-1051", "NC-1052", "NC-1056", "NC-1090", "NC-1091", "NC-1092", "NC-1093", "NC-1094", "NC-1095",
		"NC-1096", "NC-1097", "NC-1096", "NC-1098", "NC-1099", "NC-1100", "NC-1101", "NC-1102", "NC-1103", "NC-1104",
		"NC-1007", "NC-1014", "NC-1105", "NC-1106", "NC-1107",
		"NC-1108", "NC-1108", "NC-1109", "NC-1110", "NC-1110", "NC-1111", "NC-1111",
	}
	slices.Sort(want)
	if !slices.Equal(codes, want) {
		t.Fatalf("the refused project was refused with %v; want %v", codes, want)
	}
	for _, each := range []struct {
		file string
		want []string
	}{
		{file: "refused_directives.go", want: wrongDirectiveRefusals()},
		{file: "refused_markings.go", want: undecidedMarkingRefusals()},
		{file: "refused_fields.go", want: misnamedFieldRefusals()},
	} {
		placed := filepath.Join(filepath.Dir(project(t, "refused")), "routes", each.file) + ":"
		read := []string{}
		for _, line := range refused.Lines() {
			outcome.ForEachOption(outcome.TupleToOption(strings.CutPrefix(line, placed)), func(at string) { read = append(read, at) })
		}
		if !slices.Equal(read, each.want) {
			t.Errorf("%s was refused as\n%s\nwant\n%s", each.file, strings.Join(read, "\n"), strings.Join(each.want, "\n"))
		}
	}
}

// misnamedFieldRefusals is each field of the refused project whose tag or name is refused, each for
// its own cause, and never under a name nothing checked.
func misnamedFieldRefusals() []string {
	return []string{
		`19:2: NC-1110 The tag of the field Unquoted, json:item, is not written key:"value" with a space between pairs.`,
		`20:2: NC-1109 The field name it's holds ', which encoding/json does not write in a name; a name holds letters, digits, spaces and ASCII punctuation other than quotes, backquotes, backslashes and commas.`,
		`22:2: NC-1111 The field Second is written under the name twin, which the field First already takes.`,
		`30:2: NC-1108 The field is given an empty name.`,
		`31:2: NC-1108 The field is given an empty name.`,
		`33:2: NC-1111 The field Order is written under the name sort, which the field Sort already takes.`,
		`34:2: NC-1110 The tag of the field Bare, query:page, is not written key:"value" with a space between pairs.`,
	}
}

// undecidedMarkingRefusals is each refusal of the refused project's outcomes whose declaration or
// ErGo marking decides nothing, each for its own cause.
func undecidedMarkingRefusals() []string {
	return []string{
		`25:6: NC-1014 Unmarked is marked neither //ergo:code nor //ergo:not-an-error; mark it with one.`,
		`32:6: NC-1106 DoublyMarked is marked both //ergo:code RF-1006 and //ergo:not-an-error; keep one.`,
		`39:6: NC-1107 CodedTwice is marked //ergo:code more than once, with RF-1007, RF-1008; a failure has one code.`,
		`52:6: NC-1105 The outcome OpenOutcome is not marked //sumtype:decl.`,
		`70:6: NC-1007 The outcome ExportedOutcome is not sealed: its methods (example.com/refused/routes.ExportedOutcome).IsExportedOutcome are all exported; give it an unexported method.`,
	}
}

// wrongDirectiveRefusals is each wrong //nocontract: line of the refused project, refused for its own
// fault, naming the text it read.
func wrongDirectiveRefusals() []string {
	return []string{
		`11:1: NC-1096 The line //nocontract:route GET is not one method and one path.`,
		`16:1: NC-1090 The route's path unrooted does not start with /.`,
		`21:1: NC-1097 FETCH in the line //nocontract:route FETCH /fetched is not a method a route answers: GET, POST, PUT, PATCH or DELETE.`,
		`26:1: NC-1096 The line //nocontract:route GET /wordy now is not one method and one path.`,
		`31:1: NC-1091 The route's path /unnamed/{} holds a parameter without a name.`,
		`36:1: NC-1092 The route's path /ended/{$} holds the end-of-path wildcard $; a route's path ends where its last segment does.`,
		`41:1: NC-1093 The route's path /files/{path...} holds {path...}, which takes every segment left; a parameter takes one segment.`,
		`46:1: NC-1094 The route's path /braced/{a{b} holds {a{b}, a parameter whose name holds a brace.`,
		`51:1: NC-1095 The route's path /braced/a{b} holds a{b}; a segment is text holding no brace, or one parameter name in braces.`,
		`64:1: NC-1099 ok in the line //nocontract:status ok on Worded is not a number.`,
		`70:1: NC-1098 The line //nocontract:status 200 OK on Explained is not one status.`,
		`77:1: NC-1100 Restated has a second status line, //nocontract:status 201; a member answers one status.`,
		`83:1: NC-1052 204 is no HTTP status a member answers with.`,
		`99:1: NC-1101 The line //nocontract:guard Crowd Extra on a guard's refusal is not one guard name.`,
		`115:1: NC-1102 The line //nocontract:guard Staff on a handler is not one guard name and one argument.`,
		`121:1: NC-1103 staff in the line //nocontract:guard staff admin is not an exported Go identifier, the form a guard's name takes.`,
		`128:1: NC-1104 The line //nocontract:guard Staff owner is a second guard line; a declaration names one guard.`,
	}
}
