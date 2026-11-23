package running_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/internal/projectfile"
	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

type shopGeneratedFiles struct {
	client   shopSourceFile
	document shopSourceFile
}

type shopFileComparison struct {
	artifact string
	expected shopSourceFile
	actual   shopSourceFile
}

func prepareShopClient(t *testing.T, checkout string, generated shopSourceFile) string {
	t.Helper()
	fixture := readShopFixture(t, filepath.Join(checkout, "testdata", "go", "shop", "web"))
	directory := t.TempDir()
	sources := []shopSourceFile{}
	for _, file := range fixture.files {
		if file.path == filepath.Join("src", "shop_api.generated.ts") {
			continue
		}
		sources = append(sources, file)
	}
	sources = append(sources, shopSourceFile{path: filepath.Join("src", "shop_api.generated.ts"), content: generated.content})
	copyShopSources(t, shopFixture{directory: fixture.directory, files: sources}, directory)
	install := exec.CommandContext(t.Context(), "bun", "install", "--frozen-lockfile")
	install.Dir = directory
	runShopCommand(t, install)
	check := exec.CommandContext(t.Context(), "bun", "run", "--no-install", "typecheck")
	check.Dir = directory
	runShopCommand(t, check)
	return directory
}

func readShopGeneratedFiles(t *testing.T, projectPath string) shopGeneratedFiles {
	t.Helper()
	return projectfile.Read(projectPath, func(project projectfile.Project) shopGeneratedFiles {
		client := outcome.FoldOption(project.TypeScriptFile(), func(path string) shopSourceFile {
			return shopSourceFile{path: path, content: scratchproject.BytesOrStop(t, "reading the generated client "+path, outcome.TupleToResult(os.ReadFile(path)))}
		}, func() shopSourceFile {
			t.Fatalf("the shop project %s declares no TypeScript output", projectPath)
			return shopSourceFile{path: projectPath, content: []byte{}}
		})
		document := shopSourceFile{path: project.OpenAPI().File(), content: scratchproject.BytesOrStop(t, "reading the generated OpenAPI "+project.OpenAPI().File(), outcome.TupleToResult(os.ReadFile(project.OpenAPI().File())))}
		return shopGeneratedFiles{client: client, document: document}
	}, func(cause error) shopGeneratedFiles {
		t.Fatal(strings.Join((running.ProjectUnreadable{Path: projectPath, Cause: cause}).Lines(), "\n"))
		return shopGeneratedFiles{client: shopSourceFile{path: projectPath, content: []byte{}}, document: shopSourceFile{path: projectPath, content: []byte{}}}
	}, func(refusals []assembling.Refusal) shopGeneratedFiles {
		t.Fatal(strings.Join((running.ProjectFileRefused{Path: projectPath, Refusals: refusals}).Lines(), "\n"))
		return shopGeneratedFiles{client: shopSourceFile{path: projectPath, content: []byte{}}, document: shopSourceFile{path: projectPath, content: []byte{}}}
	})
}

func assertShopGeneratedFile(t *testing.T, compared shopFileComparison) {
	t.Helper()
	if bytes.Equal(compared.expected.content, compared.actual.content) {
		t.Logf("%s matches: %d bytes", compared.artifact, len(compared.actual.content))
		return
	}
	expected, actual := bytes.Split(compared.expected.content, []byte("\n")), bytes.Split(compared.actual.content, []byte("\n"))
	for index := range min(len(expected), len(actual)) {
		if !bytes.Equal(expected[index], actual[index]) {
			t.Errorf("%s differs at line %d (-%s +%s):\nwant: %s\n got: %s", compared.artifact, index+1, compared.expected.path, compared.actual.path, expected[index], actual[index])
			return
		}
	}
	t.Errorf("%s differs after line %d (-%s +%s): want %d bytes, got %d", compared.artifact, min(len(expected), len(actual)), compared.expected.path, compared.actual.path, len(compared.expected.content), len(compared.actual.content))
}

func assertShopClientAnswers(t *testing.T, directory string, served shopServerAddress, bodyLimitSupport shopBodyLimitSupport) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "bun", "run", "--no-install", "src/round_trip.ts")
	command.Dir = directory
	command.Env = append(os.Environ(), "SHOP_URL="+served.url)
	switch bodyLimitSupport.(type) {
	case shopBodyLimitEnforced:
		command.Env = append(command.Env, "SHOP_BODY_LIMIT_SUPPORT=enforced")
	case shopBodyLimitAwaitingMist:
		command.Env = append(command.Env, "SHOP_BODY_LIMIT_SUPPORT=awaiting-mist")
	case nil:
		t.Fatal("the backend has no declared body-limit support")
	}
	decoded := runShopCommand(t, command)
	t.Logf("Decoded shop answers: %s", bytes.TrimSpace(decoded))
}

func runShopCommand(t *testing.T, command *exec.Cmd) []byte {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr
	started := time.Now()
	completed := outcome.EmptyableToOption(command.Run())
	t.Logf("%s: %s", strings.Join(command.Args, " "), time.Since(started))
	outcome.ForEachOption(completed, func(cause error) {
		t.Fatalf("%s in %s: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(command.Args, " "), command.Dir, cause, stdout, stderr)
	})
	return stdout.Bytes()
}
