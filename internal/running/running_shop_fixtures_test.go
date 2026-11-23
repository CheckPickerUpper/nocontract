package running_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/outcome"
	"github.com/creachadair/atomicfile"

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
	const relativeCollection = `"nocontract": "../../.."`
	for _, file := range fixture.files {
		content := file.content
		if file.path == "nocontract.json" && bytes.Contains(content, []byte(relativeCollection)) {
			collection := filepath.Clean(filepath.Join(fixture.directory, "..", "..", ".."))
			readShopFixture(t, filepath.Join(collection, "odin", "runtime"))
			content = bytes.ReplaceAll(content, []byte(relativeCollection), []byte(`"nocontract": `+strconv.Quote(collection)))
		}
		scratchproject.WriteFile(t, directory, file.path, content)
	}
}

func copyGleamShop(t *testing.T, fixture shopFixture) string {
	t.Helper()
	project := copyShop(t, fixture)
	directory := filepath.Dir(project)
	runtimeDirectory := filepath.Clean(filepath.Join(fixture.directory, "..", "..", "..", "gleam", "runtime"))
	runtime := readShopFixture(t, runtimeDirectory).directory
	configuration := heldOrStop(t, filepath.Join(fixture.directory, "gleam.toml"))
	manifest := heldOrStop(t, filepath.Join(fixture.directory, "manifest.toml"))
	compiler := exec.CommandContext(t.Context(), "gleam", "--version")
	compiler.Dir = directory
	version := scratchproject.BytesOrStop(t, "reading the Gleam version", outcome.TupleToResult(compiler.Output()))
	inputs := [][]byte{
		version,
		[]byte(configuration),
		[]byte(manifest),
		[]byte(heldOrStop(t, filepath.Join(runtime, "gleam.toml"))),
	}
	key := sha256.Sum256(scratchproject.BytesOrStop(t, "encoding the Gleam dependency inputs", outcome.TupleToResult(json.Marshal(inputs))))
	cacheDirectory := scratchproject.PathOrStop(t, "locating the dependency cache", outcome.TupleToResult(os.UserCacheDir()))
	cachedFingerprint := filepath.Join(cacheDirectory, "nocontract", "gleam-shop-dependencies", fmt.Sprintf("%x.config_fingerprint", key))
	for _, file := range []shopSourceFile{
		{path: "gleam.toml", content: []byte(configuration)},
		{path: "manifest.toml", content: []byte(manifest)},
	} {
		content := strings.ReplaceAll(string(file.content), `"../../../gleam/runtime"`, strconv.Quote(runtime))
		scratchproject.WriteFile(t, directory, file.path, []byte(content))
	}
	fingerprint := filepath.Join("build", "packages", "nocontract_runtime.config_fingerprint")
	return outcome.Fold(outcome.TupleToResult(os.ReadFile(cachedFingerprint)), func(content []byte) string {
		scratchproject.WriteFile(t, directory, fingerprint, content)
		return project
	}, func(readError error) string {
		if !errors.Is(readError, os.ErrNotExist) {
			t.Fatalf("reading the Gleam dependency fingerprint %s: %v", cachedFingerprint, readError)
		}
		declaredManifest := heldOrStop(t, filepath.Join(directory, "manifest.toml"))
		dependencies := exec.CommandContext(t.Context(), "gleam", "deps", "download")
		dependencies.Dir = directory
		output := &bytes.Buffer{}
		dependencies.Stdout, dependencies.Stderr = output, output
		outcome.ForEachOption(outcome.EmptyableToOption(dependencies.Run()), func(cause error) {
			t.Fatalf("preparing the Gleam dependencies in %s: %v\n%s", directory, cause, output)
		})
		// Gleam writes the fingerprint before resolving. Publish only a successful
		// resolution that preserves the lock whose bytes identify this cache entry.
		if heldOrStop(t, filepath.Join(directory, "manifest.toml")) != declaredManifest {
			return project
		}
		content := scratchproject.BytesOrStop(t, "reading the resolved Gleam dependency fingerprint", outcome.TupleToResult(os.ReadFile(filepath.Join(directory, fingerprint))))
		scratchproject.StopOn(t, "making the Gleam dependency cache", os.MkdirAll(filepath.Dir(cachedFingerprint), 0o700))
		scratchproject.StopOn(t, "publishing the Gleam dependency fingerprint", atomicfile.WriteData(cachedFingerprint, content, 0o600))
		return project
	})
}
