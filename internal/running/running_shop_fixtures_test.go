package running_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

type shopSourceFile struct {
	path    string
	content []byte
}

type shopFixture struct {
	directory string
	files     []shopSourceFile
}

// Source reads expose the compilers' fixture inputs to Go's test cache. Native copies contain
// authored sources and manifests; compiler output and generated contracts stay in test scratch.
func readShopFixture(t *testing.T, directory string) shopFixture {
	t.Helper()
	files := []shopSourceFile{}
	walked := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, readError error) error {
		return outcome.FoldOption(outcome.EmptyableToOption(readError), func(cause error) error { return cause }, func() error {
			if entry.IsDir() {
				switch entry.Name() {
				case ".scratch", "build", "web", "node_modules":
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".odin", ".gleam", ".erl", ".toml", ".mod", ".sum":
			case ".json":
				if entry.Name() != "nocontract.json" {
					return nil
				}
			default:
				return nil
			}
			relative := scratchproject.PathOrStop(t, "locating fixture source "+path, outcome.TupleToResult(filepath.Rel(directory, path)))
			content := scratchproject.BytesOrStop(t, "reading fixture source "+path, outcome.TupleToResult(os.ReadFile(path)))
			files = append(files, shopSourceFile{path: relative, content: content})
			return nil
		})
	})
	scratchproject.StopOn(t, "reading shop fixture "+directory, walked)
	return shopFixture{directory: directory, files: files}
}

func copyGoShop(t *testing.T, fixture shopFixture) string {
	t.Helper()
	project := copyShop(t, fixture)
	directory := filepath.Dir(project)
	checkout := filepath.Clean(filepath.Join(fixture.directory, "..", "..", ".."))
	module := heldOrStop(t, filepath.Join(directory, "go.mod"))
	scratchproject.WriteFile(t, directory, "go.mod", scratchproject.BytesOrStop(t, "pointing the shop at NoContract", scratchproject.LocalModule([]byte(module), checkout)))

	projectFields := map[string]json.RawMessage{}
	scratchproject.StopOn(t, "reading the Go shop test project", json.Unmarshal([]byte(heldOrStop(t, project)), &projectFields))
	delete(projectFields, "typescript")
	config := scratchproject.BytesOrStop(t, "encoding the Go shop test project", outcome.TupleToResult(json.Marshal(projectFields)))
	scratchproject.WriteFile(t, directory, filepath.Base(project), config)
	return project
}

func copyShop(t *testing.T, fixture shopFixture) string {
	t.Helper()
	directory := t.TempDir()
	for _, file := range fixture.files {
		scratchproject.WriteFile(t, directory, file.path, file.content)
	}
	return filepath.Join(directory, "nocontract.json")
}

func copyGleamShop(t *testing.T, fixture shopFixture) string {
	t.Helper()
	project := copyShop(t, fixture)
	directory := filepath.Dir(project)
	runtimeDirectory := filepath.Clean(filepath.Join(fixture.directory, "..", "..", "..", "gleam", "runtime"))
	runtime := readShopFixture(t, runtimeDirectory).directory
	for _, name := range []string{"gleam.toml", "manifest.toml"} {
		manifest := strings.ReplaceAll(heldOrStop(t, filepath.Join(directory, name)), `"../../../gleam/runtime"`, strconv.Quote(runtime))
		scratchproject.WriteFile(t, directory, name, []byte(manifest))
	}
	return project
}
