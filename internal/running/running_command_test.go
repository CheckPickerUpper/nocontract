package running_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/CheckPickerUpper/outcome"

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

// A project file that names no OpenAPI file, or no version for it, is refused before any package
// is read, each with its own code.
func TestAProjectFileMissingItsOpenAPIFileOrVersionIsRefused(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		name    string
		openAPI string
		want    ergo.Code
	}{
		{name: "no openapi object", openAPI: "", want: running.OpenAPIFileMissing{}.ErrorCode()},
		{name: "no file", openAPI: `, "openapi": {"version": "1.0.0"}`, want: running.OpenAPIFileMissing{}.ErrorCode()},
		{name: "no version", openAPI: `, "openapi": {"file": "shop_api.openapi.json"}`, want: running.OpenAPIVersionMissing{}.ErrorCode()},
	} {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()
			project := filepath.Join(t.TempDir(), "nocontract.json")
			content := `{"language": "go", "packages": ["./..."], "typescript": "shop_api.generated.ts", "api": "ShopApi"` + each.openAPI + "}"
			outcome.ForEachOption(outcome.EmptyableToOption(os.WriteFile(project, []byte(content), 0o644)), func(cause error) { t.Fatalf("writing %s: %v", project, cause) })
			checked := running.Run(running.Request{Arguments: []string{"check", "-project", project}, Context: t.Context()})
			unreadable, isUnreadable := checked.(running.ProjectUnreadable)
			if !isUnreadable {
				t.Fatalf("check answered %#v; want the project file refused", checked)
			}
			coded, isCoded := unreadable.Cause.(interface{ ErrorCode() ergo.Code })
			if !isCoded {
				t.Fatalf("the project file was refused with %v, which has no code; want %s", unreadable.Cause, each.want)
			}
			if got := coded.ErrorCode(); got != each.want {
				t.Fatalf("the project file was refused with %s (%v); want %s", got, unreadable.Cause, each.want)
			}
		})
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
