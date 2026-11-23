package reading_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/google/go-cmp/cmp"
)

func TestHeaderTagsRefuseEveryAmbiguousBinding(t *testing.T) {
	directory := scratchproject.Ledger(t, "../..")
	want := []assembling.Refusal{}
	for _, declaration := range []struct {
		name     string
		field    string
		tags     string
		problems func(string) []assembling.Problem
	}{
		{name: "BodyHeaderConflict", field: "Body", tags: `header:"X-Order-Count"`, problems: func(string) []assembling.Problem {
			return []assembling.Problem{assembling.HeaderBindingConflict{Name: "X-Order-Count", Field: "Body", Part: "body", Binding: "Body"}}
		}},
		{name: "CaseEquivalentHeaders", field: "ShopperID", tags: `header:"X-Order-Count" header:"x-ORDER-count"`, problems: func(first string) []assembling.Problem {
			return []assembling.Problem{assembling.HeaderNameRepeated{Name: "x-ORDER-count", Field: "ShopperID", FirstName: "X-Order-Count", FirstField: "ShopperID", FirstPosition: first}}
		}},
		{name: "DifferentHeaderNames", field: "ShopperID", tags: `header:"X-Order-Count" header:"X-Other-Count"`, problems: func(first string) []assembling.Problem {
			return []assembling.Problem{assembling.HeaderFieldRepeated{Field: "ShopperID", Name: "X-Other-Count", FirstName: "X-Order-Count", FirstPosition: first}}
		}},
		{name: "EveryHeaderAlias", field: "ShopperID", tags: `header:"X-Order-Count" header:"X-Other-Count" header:"X-Third-Count"`, problems: func(first string) []assembling.Problem {
			return []assembling.Problem{
				assembling.HeaderFieldRepeated{Field: "ShopperID", Name: "X-Other-Count", FirstName: "X-Order-Count", FirstPosition: first},
				assembling.HeaderFieldRepeated{Field: "ShopperID", Name: "X-Third-Count", FirstName: "X-Order-Count", FirstPosition: first},
			}
		}},
		{name: "PathHeaderConflict", field: "ShopperID", tags: `header:"X-Order-Count" path:"shopper_id"`, problems: func(string) []assembling.Problem {
			return []assembling.Problem{assembling.HeaderBindingConflict{Name: "X-Order-Count", Field: "ShopperID", Part: "path", Binding: "shopper_id"}}
		}},
		{name: "QueryHeaderConflict", field: "ShopperID", tags: `header:"X-Order-Count" query:"count"`, problems: func(string) []assembling.Problem {
			return []assembling.Problem{assembling.HeaderBindingConflict{Name: "X-Order-Count", Field: "ShopperID", Part: "query", Binding: "count"}}
		}},
	} {
		source := strings.NewReplacer(
			`path:"shopper_id"`, declaration.tags,
			"ShopperID", declaration.field,
			"ReadInput", declaration.name+"Input",
			"ReadOutcome", declaration.name+"Outcome",
			"BalanceRead", declaration.name+"Read",
			"ReadBalance", "Read"+declaration.name,
			"/shoppers/{shopper_id}/balance", "/headers/"+declaration.name,
		).Replace(scratchproject.LedgerRoutes)
		file := declaration.name + ".go"
		scratchproject.WriteFile(t, directory, "ledger/"+file, []byte(source))
		position := assembling.Position{File: filepath.Join(directory, "ledger", file), Line: 8, Column: 2}
		for _, problem := range declaration.problems(position.String()) {
			want = append(want, assembling.Refusal{Position: position, Problem: problem})
		}
	}
	scratchproject.StopOn(t, "removing the seed route", os.Remove(filepath.Join(directory, "ledger", "ledger_routes.go")))
	generated := running.Run(running.Request{Arguments: []string{"generate", "-project", filepath.Join(directory, "nocontract.json")}, Context: t.Context()})
	refused, exact := generated.(running.DeclarationsRefused)
	if !exact {
		t.Fatalf("generating the header declarations: %s", strings.Join(generated.Lines(), "\n"))
	}
	if difference := cmp.Diff(want, refused.Refusals); difference != "" {
		t.Errorf("header declaration refusals differ (-want +got):\n%s", difference)
	}
}
