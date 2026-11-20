package running_test

import (
	"io/fs"
	"os"
	"path/filepath"
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
			case ".go", ".odin", ".gleam", ".toml", ".mod", ".sum":
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

func useAuthoredGoShop(t *testing.T, fixture shopFixture) string {
	t.Helper()
	return filepath.Join(fixture.directory, "nocontract.json")
}

func copyNativeShop(t *testing.T, fixture shopFixture) string {
	t.Helper()
	directory := t.TempDir()
	for _, file := range fixture.files {
		scratchproject.WriteFile(t, directory, file.path, file.content)
	}
	return filepath.Join(directory, "nocontract.json")
}
