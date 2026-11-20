package running_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/internal/running"
)

// projectFileLines is a project file that names every key once, one key to a line, with the line
// of each key in changed written as given instead, and dropped when given as "".
func projectFileLines(changed map[string]string) string {
	lines := []string{"{"}
	for _, key := range []string{"language", "packages", "typescript", "api", "openapi", "server"} {
		line := map[string]string{
			"language": `"language": "go",`, "packages": `"packages": ["./..."],`, "typescript": `"typescript": "web/api.generated.ts",`,
			"api": `"api": "ShopApi",`, "openapi": `"openapi": {"file": "api.openapi.json", "version": "1.0.0"},`, "server": `"server": "server"`,
		}[key]
		if written, isChanged := changed[key]; isChanged {
			line = written
		}
		if line != "" {
			lines = append(lines, "  "+line)
		}
	}
	return strings.Join(append(lines, "}"), "\n") + "\n"
}

// Each way a project file is wrong is refused with its own code at the place it is written, naming
// the key and the value read, before any package is read; every place is refused at once.
func TestAProjectFileIsRefusedAtEachPlaceItIsWrong(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "an API name TypeScript cannot declare a class as",
			content: projectFileLines(map[string]string{"api": `"api": "shop-api",`}),
			want:    []string{"5:10: NC-1081 shop-api cannot name the API's class in TypeScript: a later character cannot be part of an identifier."},
		},
		{
			name:    "an API name that is a reserved word",
			content: projectFileLines(map[string]string{"api": `"api": "class",`}),
			want:    []string{"5:10: NC-1081 class cannot name the API's class in TypeScript: it is a reserved word, or a type's name a class cannot take."},
		},
		{
			name:    "a missing key",
			content: projectFileLines(map[string]string{"language": ""}),
			want:    []string{"1:1: NC-1084 The project file has no language."},
		},
		{
			name:    "a value of the wrong type",
			content: projectFileLines(map[string]string{"server": `"server": 3`}),
			want:    []string{"7:13: NC-1085 server is 3, where it must be a string."},
		},
		{
			name:    "an empty value",
			content: projectFileLines(map[string]string{"server": `"server": ""`}),
			want:    []string{`7:13: NC-1086 server is "", which names nothing.`},
		},
		{
			name:    "no package patterns",
			content: projectFileLines(map[string]string{"packages": `"packages": [],`}),
			want:    []string{"3:15: NC-1086 packages is [], which names nothing."},
		},
		{
			name:    "a package pattern that is not a string",
			content: projectFileLines(map[string]string{"packages": `"packages": ["./...", 1],`}),
			want:    []string{"3:25: NC-1085 packages[1] is 1, where it must be a string."},
		},
		{
			name:    "an OpenAPI file that is empty, with no version",
			content: projectFileLines(map[string]string{"openapi": `"openapi": {"file": ""},`}),
			want:    []string{"6:14: NC-1084 The project file has no openapi.version.", `6:23: NC-1086 openapi.file is "", which names nothing.`},
		},
		{
			name:    "an OpenAPI value that is not an object",
			content: projectFileLines(map[string]string{"openapi": `"openapi": "api.openapi.json",`}),
			want:    []string{`6:14: NC-1085 openapi is "api.openapi.json", where it must be an object.`},
		},
		{
			name:    "a key no project file holds",
			content: projectFileLines(map[string]string{"server": `"servers": "server"`}),
			want:    []string{"1:1: NC-1084 The project file has no server.", "7:3: NC-1087 servers is not a key a project file holds."},
		},
		{
			name:    "a key written twice",
			content: projectFileLines(map[string]string{"server": `"server": "server", "server": "other"`}),
			want:    []string{"7:23: NC-1088 server is written more than once."},
		},
		{
			name:    "a language NoContract does not read",
			content: projectFileLines(map[string]string{"language": `"language": "rust",`}),
			want:    []string{`2:15: NC-1055 The project file names the language "rust". NoContract reads go, odin, gleam.`},
		},
		{
			name:    "a value that is not an object",
			content: "[]\n",
			want:    []string{"1:1: NC-1083 The project file holds [], where it must hold an object."},
		},
		{
			name:    "content that is not JSON",
			content: "{\"language\" \"go\"}\n",
			want:    []string{"1:13: NC-1082 The project file is not JSON: invalid character '\"' after object key."},
		},
	} {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()
			project := filepath.Join(t.TempDir(), "nocontract.json")
			outcome.ForEachOption(outcome.EmptyableToOption(os.WriteFile(project, []byte(each.content), 0o644)), func(cause error) { t.Fatalf("writing %s: %v", project, cause) })
			checked := running.Run(running.Request{Arguments: []string{"check", "-project", project}, Context: t.Context()})
			want := []string{"NC-1089 The project file " + project + " describes no project:"}
			for _, line := range each.want {
				want = append(want, project+":"+line)
			}
			if got := checked.Lines(); checked.ExitCode() != 2 || !slices.Equal(got, want) {
				t.Fatalf("check exited %d with\n%s\nwant 2 with\n%s", checked.ExitCode(), strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}
