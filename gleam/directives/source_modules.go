package directives

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/outcome"
)

// SourceModule identifies an authored module, its source dependencies and directive comments.
// The compiler still decides which comments attach to public declarations.
type SourceModule struct {
	Name       string
	Path       string
	Imports    []string
	Directives []string
}

// Sources reads source modules without compiling generated server or startup modules.
func Sources(directory string) outcome.Result[map[string]SourceModule] {
	source := filepath.Join(directory, "src")
	modules := map[string]SourceModule{}
	walked := filepath.WalkDir(source, func(path string, entry fs.DirEntry, cause error) error {
		return outcome.FoldOption(outcome.EmptyableToOption(cause), func(cause error) error {
			return SourceUnreadable{Path: path, Cause: cause}
		}, func() error {
			if entry.IsDir() || filepath.Ext(path) != ".gleam" {
				return nil
			}
			return outcome.Fold(outcome.TupleToResult(os.ReadFile(path)), func(content []byte) error {
				if bytes.HasPrefix(content, []byte(description.GeneratedHeader)) {
					return nil
				}
				return outcome.Fold(outcome.TupleToResult(filepath.Rel(source, path)), func(relative string) error {
					name := strings.TrimSuffix(filepath.ToSlash(relative), ".gleam")
					modules[name] = sourceModule(name, path, string(content))
					return nil
				}, func(cause error) error { return SourceUnreadable{Path: path, Cause: cause} })
			}, func(cause error) error { return SourceUnreadable{Path: path, Cause: cause} })
		})
	})
	return outcome.TupleToResult(modules, walked)
}

func sourceModule(name, path, content string) SourceModule {
	module := SourceModule{Name: name, Path: path, Imports: []string{}, Directives: []string{}}
	tokens := sourceTokens.FindAllString(content, -1)
	for index, token := range tokens {
		switch {
		case token == "import":
			module.Imports = append(module.Imports, importedModule(tokens[index+1:]))
		case strings.HasPrefix(token, "///") && strings.Contains(token, "nocontract:"):
			module.Directives = append(module.Directives, token)
			text := strings.TrimSpace(strings.TrimPrefix(token, "///"))
			if after, guard := strings.CutPrefix(text, "nocontract:guard "); guard {
				fields := strings.Fields(after)
				if len(fields) > 0 {
					guardModule, _, _ := strings.Cut(fields[0], ".")
					module.Imports = append(module.Imports, guardModule)
				}
			}
		}
	}
	return module
}

func importedModule(tokens []string) string {
	parts := []string{}
	for index, token := range tokens {
		if index%2 == 0 {
			if token == "" || token[0] == '/' || token[0] == '"' || token == "." || token == "{" {
				return strings.Join(parts, "/")
			}
			parts = append(parts, token)
			continue
		}
		if token != "/" {
			return strings.Join(parts, "/")
		}
	}
	return strings.Join(parts, "/")
}
