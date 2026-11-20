// Package effect generates a TypeScript HttpApi client from a NoContract OpenAPI document.
package effect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/outcome"
)

// Client identifies the frontend and carries the exact OpenAPI bytes NoContract publishes.
type Client struct {
	Output  string
	Name    description.APIName
	OpenAPI []byte
}

// Render runs the frontend's installed Effect generator on the OpenAPI document.
func Render(ctx context.Context, client Client) outcome.Result[[]byte] {
	return outcome.Fold(installedGenerator(filepath.Dir(client.Output)), func(program string) outcome.Result[[]byte] {
		return outcome.Fold(outcome.TupleToResult(os.CreateTemp("", "nocontract-*.openapi.json")), func(spec *os.File) outcome.Result[[]byte] {
			path := spec.Name()
			defer func() { _ = os.Remove(path) }()
			_, writeError := spec.Write(client.OpenAPI)
			prepared := outcome.TupleToResult(path, errors.Join(writeError, spec.Close()))
			return outcome.Fold(prepared, func(path string) outcome.Result[[]byte] {
				return generate(ctx, program, path, client.Name)
			}, outcome.Err[[]byte])
		}, outcome.Err[[]byte])
	}, outcome.Err[[]byte])
}

const generatorManifest = "node_modules/@effect/openapi-generator/package.json"

type packageManifest struct {
	Bin map[string]string `json:"bin"`
}

type generatorBinOutsidePackage struct{ Manifest, Bin string }

func (failure generatorBinOutsidePackage) Error() string {
	return fmt.Sprintf("openapigen entry point %q in %s is outside its package", failure.Bin, failure.Manifest)
}

type generatorBinMissing struct{ Manifest string }

func (failure generatorBinMissing) Error() string {
	return fmt.Sprintf("%s does not declare the openapigen executable", failure.Manifest)
}

type generatorOutputEmpty struct{ Program, API, Stderr string }

func (failure generatorOutputEmpty) Error() string {
	return fmt.Sprintf("%s emitted no client for %s: %s", failure.Program, failure.API, failure.Stderr)
}

func installedGenerator(directory string) outcome.Result[string] {
	path := filepath.Join(directory, filepath.FromSlash(generatorManifest))
	return outcome.Fold(outcome.TupleToResult(os.ReadFile(path)), func(content []byte) outcome.Result[string] {
		manifest := packageManifest{Bin: map[string]string{}}
		return outcome.Fold(outcome.TupleToResult(&manifest, json.Unmarshal(content, &manifest)), func(decoded *packageManifest) outcome.Result[string] {
			return outcome.FoldOption(keyed.Entry(decoded.Bin, "openapigen"), func(bin string) outcome.Result[string] {
				if filepath.IsLocal(bin) {
					return outcome.Ok(filepath.Join(filepath.Dir(path), bin))
				}
				return outcome.Err[string](generatorBinOutsidePackage{Manifest: path, Bin: bin})
			}, func() outcome.Result[string] {
				return outcome.Err[string](generatorBinMissing{Manifest: path})
			})
		}, func(cause error) outcome.Result[string] {
			return outcome.Err[string](fmt.Errorf("reading %s: %w", path, cause))
		})
	}, func(cause error) outcome.Result[string] {
		parent := filepath.Dir(directory)
		if errors.Is(cause, fs.ErrNotExist) && parent != directory {
			return outcome.Fold(installedGenerator(parent), outcome.Ok[string], func(cause error) outcome.Result[string] {
				return outcome.Err[string](fmt.Errorf("also searched %s: %w", path, cause))
			})
		}
		return outcome.Err[string](fmt.Errorf("locating the frontend's installed @effect/openapi-generator at %s: %w", path, cause))
	})
}

func generate(ctx context.Context, program, spec string, name description.APIName) outcome.Result[[]byte] {
	command := exec.CommandContext(ctx, "bun", program, "--format", "httpapi", "--string-formats", "decoded", "--spec", spec, "--name", name.Text())
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	return outcome.Fold(outcome.TupleToResult(command.Output()), func(client []byte) outcome.Result[[]byte] {
		if len(bytes.TrimSpace(client)) == 0 {
			return outcome.Err[[]byte](generatorOutputEmpty{Program: program, API: name.Text(), Stderr: strings.TrimSpace(stderr.String())})
		}
		return outcome.Ok(append([]byte(description.GeneratedHeader+"\n"), client...))
	}, func(cause error) outcome.Result[[]byte] {
		return outcome.Err[[]byte](fmt.Errorf("%s: %w\n%s", strings.Join(command.Args, " "), cause, strings.TrimSpace(stderr.String())))
	})
}
