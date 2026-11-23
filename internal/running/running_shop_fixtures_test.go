package running_test

import (
	"io/fs"
	"os"
	"os/exec"
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
// authored sources and manifests, and generated contracts stay in test scratch.
func readShopFixture(t *testing.T, directory string) shopFixture {
	t.Helper()
	files := []shopSourceFile{}
	walked := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, readError error) error {
		return outcome.FoldOption(outcome.EmptyableToOption(readError), func(cause error) error { return cause }, func() error {
			if entry.IsDir() {
				if path != directory {
					switch entry.Name() {
					case ".scratch", "build", "web", "node_modules":
						return filepath.SkipDir
					}
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".odin", ".gleam", ".erl", ".toml", ".mod", ".sum", ".ts", ".lock":
			case ".json":
				switch entry.Name() {
				case "nocontract.json", "package.json", "tsconfig.json":
				default:
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
	return project
}

func copyShop(t *testing.T, fixture shopFixture) string {
	t.Helper()
	directory := t.TempDir()
	copyShopSources(t, fixture, directory)
	return filepath.Join(directory, "nocontract.json")
}

func copyShopSources(t *testing.T, fixture shopFixture, directory string) {
	t.Helper()
	for _, file := range fixture.files {
		scratchproject.WriteFile(t, directory, file.path, file.content)
	}
}

func copyGleamShop(t *testing.T, fixture shopFixture) string {
	t.Helper()
	// Gleam checks its path dependency fingerprint separately from the locked versions
	// and global tarballs. Reuse its prepared metadata so a new test directory keeps
	// the existing resolution.
	dependencies := exec.CommandContext(t.Context(), "gleam", "deps", "download")
	dependencies.Dir = fixture.directory
	if output, cause := dependencies.CombinedOutput(); cause != nil {
		t.Fatalf("preparing the Gleam dependencies in %s: %v\n%s", fixture.directory, cause, output)
	}

	project := copyShop(t, fixture)
	directory := filepath.Dir(project)
	runtimeDirectory := filepath.Clean(filepath.Join(fixture.directory, "..", "..", "..", "gleam", "runtime"))
	runtime := readShopFixture(t, runtimeDirectory).directory
	for _, name := range []string{"gleam.toml", "manifest.toml"} {
		manifest := strings.ReplaceAll(heldOrStop(t, filepath.Join(fixture.directory, name)), `"../../../gleam/runtime"`, strconv.Quote(runtime))
		scratchproject.WriteFile(t, directory, name, []byte(manifest))
	}
	fingerprint := filepath.Join("build", "packages", "nocontract_runtime.config_fingerprint")
	cachedFingerprint := filepath.Join(fixture.directory, fingerprint)
	info, cause := os.Stat(cachedFingerprint)
	scratchproject.StopOn(t, "reading the Gleam dependency fingerprint's modification time", cause)
	scratchproject.WriteFile(t, directory, fingerprint, []byte(heldOrStop(t, cachedFingerprint)))
	scratchproject.StopOn(t, "preserving the Gleam dependency fingerprint's modification time",
		os.Chtimes(filepath.Join(directory, fingerprint), info.ModTime(), info.ModTime()))
	return project
}
