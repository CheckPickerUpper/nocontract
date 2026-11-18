package running_test

import (
	"path/filepath"
	"slices"
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
	outcome := check(t, "refused")
	refused, isRefused := outcome.(running.DeclarationsRefused)
	if !isRefused {
		t.Fatalf("check answered %#v; want the refused project's declarations refused", outcome)
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
		"NC-1004", "NC-1005", "NC-1007", "NC-1012", "NC-1013", "NC-1014", "NC-1015", "NC-1016", "NC-1017", "NC-1018",
		"NC-1022", "NC-1024", "NC-1025", "NC-1027", "NC-1029", "NC-1032", "NC-1033", "NC-1045", "NC-1046", "NC-1048", "NC-1049", "NC-1050",
		"NC-1051", "NC-1051", "NC-1056",
	}
	if !slices.Equal(codes, want) {
		t.Fatalf("the refused project was refused with %v; want %v", codes, want)
	}
}
