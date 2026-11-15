package running_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/CheckPickerUpper/nocontract/internal/running"
)

func check(ctx context.Context, project string) running.Outcome {
	return running.Run(running.Request{
		Arguments: []string{"check", "-project", filepath.Join("..", "..", "testdata", "go", project, "nocontract.json")},
		Context:   ctx,
	})
}

// The example shop's committed Go server file and Effect API are what generate writes from its
// routes, so a change to the reader or either writer that changes a file fails here until the
// files are generated again, the shop builds, and its TypeScript is checked.
func TestTheShopsCommittedFilesAreWhatGenerateWrites(t *testing.T) {
	t.Parallel()
	outcome := check(t.Context(), "shop")
	if _, isCurrent := outcome.(running.FilesCurrent); !isCurrent {
		t.Fatalf("check answered %s; want the shop's files current", outcome.Report())
	}
}

func TestEachWrongDeclarationIsRefusedWithItsCode(t *testing.T) {
	t.Parallel()
	outcome := check(t.Context(), "refused")
	refused, isRefused := outcome.(running.DeclarationsRefused)
	if !isRefused {
		t.Fatalf("check answered %#v; want the refused project's declarations refused", outcome)
	}
	codes := []string{}
	for _, refusal := range refused.Refusals {
		codes = append(codes, refusal.Problem.ErrorCode().String())
	}
	slices.Sort(codes)
	want := []string{
		"NC-1004", "NC-1005", "NC-1007", "NC-1012", "NC-1013", "NC-1014", "NC-1015", "NC-1016", "NC-1017",
		"NC-1022", "NC-1024", "NC-1025", "NC-1029", "NC-1032", "NC-1033", "NC-1045", "NC-1046", "NC-1048", "NC-1049", "NC-1050", "NC-1051",
		"NC-1056",
	}
	if !slices.Equal(codes, want) {
		t.Fatalf("the refused project was refused with %v; want %v", codes, want)
	}
}
