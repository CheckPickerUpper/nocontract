package gleam

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/outcome"
	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// CompilerMetadataMalformed is a manifest or lockfile the TOML parser could not read.
//
//ergo:code NC-1145
//ergo:message The Gleam compiler metadata {Path} is malformed.
type CompilerMetadataMalformed struct {
	Path    string
	Content []byte
	Cause   error
}

func (failure CompilerMetadataMalformed) Error() string {
	return fmt.Sprintf("Gleam compiler metadata %s: %v", failure.Path, failure.Cause)
}
func (failure CompilerMetadataMalformed) Unwrap() error { return failure.Cause }

// CompilerMetadataUnreadable is a manifest or lockfile the operating system could not read.
//
//ergo:code NC-1146
//ergo:message The Gleam compiler metadata {Path} could not be read.
type CompilerMetadataUnreadable struct {
	Path  string
	Cause error
}

func (failure CompilerMetadataUnreadable) Error() string {
	return fmt.Sprintf("Gleam compiler metadata %s: %v", failure.Path, failure.Cause)
}
func (failure CompilerMetadataUnreadable) Unwrap() error { return failure.Cause }

func compilerDependencies(context context.Context, directory, view string) outcome.Result[struct{}] {
	arguments := []string{"deps", "download"}
	command := exec.CommandContext(context, "gleam", arguments...)
	command.Dir = directory
	output := &bytes.Buffer{}
	command.Stdout, command.Stderr = output, output
	started := time.Now()
	return outcome.Fold(outcome.TupleToResult(struct{}{}, command.Run()), func(struct{}) outcome.Result[struct{}] {
		return outcome.Fold(compilerMetadata(directory, view, "gleam.toml"), func([]string) outcome.Result[struct{}] {
			return outcome.Fold(compilerMetadata(directory, view, "manifest.toml"), func([]string) outcome.Result[struct{}] {
				build := filepath.Join(compilerOutputDirectory(directory), "build")
				return outcome.Fold(outcome.TupleToResult(struct{}{}, os.MkdirAll(build, 0o755)), func(struct{}) outcome.Result[struct{}] {
					return outcome.Fold(outcome.TupleToResult(struct{}{}, retainedSourceLink(filepath.Join(directory, "build", "packages"), filepath.Join(build, "packages"))), func(struct{}) outcome.Result[struct{}] {
						return outcome.TupleToResult(struct{}{}, createSourceLink(build, filepath.Join(view, "build")))
					}, outcome.Err[struct{}])
				}, outcome.Err[struct{}])
			}, outcome.Err[struct{}])
		}, outcome.Err[struct{}])
	}, func(cause error) outcome.Result[struct{}] {
		return outcome.Err[struct{}](compiler.ExportRefused{Directory: directory, Arguments: arguments, Output: output.String(), Diagnostics: "", Duration: time.Since(started), Cause: cause})
	})
}

// CompilerCacheLinkRefused is a compiler source cache link pointing at another source.
//
//ergo:code NC-1149
//ergo:message The Gleam compiler cache {Link} points at {Existing} instead of {Source}.
type CompilerCacheLinkRefused struct{ Source, Link, Existing string }

func (failure CompilerCacheLinkRefused) Error() string {
	return fmt.Sprintf("Gleam compiler source cache %s points at %s instead of %s", failure.Link, failure.Existing, failure.Source)
}

func retainedSourceLink(source, target string) error {
	cause := os.Symlink(source, target)
	if errors.Is(cause, os.ErrExist) {
		return outcome.Fold(outcome.TupleToResult(os.Readlink(target)), func(existing string) error {
			if existing != source {
				return CompilerCacheLinkRefused{Source: source, Link: target, Existing: existing}
			}
			return nil
		}, func(readError error) error { return readError })
	}
	return cause
}

type dependencyPath struct {
	Text  string
	Range unstable.Range
}

type dependencyEntry struct {
	Name   string
	Path   outcome.Option[dependencyPath]
	Git    string
	Source string
}

func emptyDependency() dependencyEntry {
	return dependencyEntry{Name: "", Path: outcome.None[dependencyPath](), Git: "", Source: ""}
}

type compilerMetadataRead struct {
	Dependencies []dependencyEntry
	Packages     []string
}

func compilerMetadata(directory, view, name string) outcome.Result[[]string] {
	path := filepath.Join(directory, name)
	return outcome.Fold(outcome.TupleToResult(os.ReadFile(path)), func(content []byte) outcome.Result[[]string] {
		return outcome.Fold(readCompilerMetadata(path, content), func(metadata compilerMetadataRead) outcome.Result[[]string] {
			rewritten := outcome.Ok(bytes.Clone(content))
			slices.SortFunc(metadata.Dependencies, func(left, right dependencyEntry) int {
				return cmp.Compare(pathOffset(right.Path), pathOffset(left.Path))
			})
			for _, dependency := range metadata.Dependencies {
				if dependency.Git != "" || dependency.Source == "git" {
					continue
				}
				rewritten = outcome.Fold(rewritten, func(content []byte) outcome.Result[[]byte] {
					return outcome.FoldOption(dependency.Path, func(local dependencyPath) outcome.Result[[]byte] {
						return outcome.Fold(rootedPathLiteral(directory, local.Text), func(literal []byte) outcome.Result[[]byte] {
							before := bytes.Clone(content[:local.Range.Offset])
							before = append(before, literal...)
							return outcome.Ok(append(before, content[local.Range.Offset+local.Range.Length:]...))
						}, outcome.Err[[]byte])
					}, func() outcome.Result[[]byte] { return outcome.Ok(content) })
				}, outcome.Err[[]byte])
			}
			return outcome.Fold(rewritten, func(content []byte) outcome.Result[[]string] {
				return outcome.TupleToResult(metadata.Packages, os.WriteFile(filepath.Join(view, name), content, 0o644))
			}, outcome.Err[[]string])
		}, outcome.Err[[]string])
	}, func(cause error) outcome.Result[[]string] {
		return outcome.Err[[]string](CompilerMetadataUnreadable{Path: path, Cause: cause})
	})
}

func pathOffset(path outcome.Option[dependencyPath]) uint32 {
	return outcome.FoldOption(path, func(path dependencyPath) uint32 { return path.Range.Offset }, func() uint32 { return 0 })
}

func rootedPathLiteral(directory, path string) outcome.Result[[]byte] {
	rooted := path
	if !filepath.IsAbs(path) {
		rooted = filepath.Join(directory, path)
	}
	return outcome.Fold(outcome.TupleToResult(toml.Marshal(struct {
		Path string `toml:"path"`
	}{Path: rooted})), func(content []byte) outcome.Result[[]byte] {
		_, literal, _ := bytes.Cut(content, []byte("="))
		return outcome.Ok(bytes.TrimSpace(literal))
	}, outcome.Err[[]byte])
}

func readCompilerMetadata(path string, content []byte) outcome.Result[compilerMetadataRead] {
	parser := unstable.Parser{KeepComments: false}
	parser.Reset(content)
	table := []string{}
	parsed := compilerMetadataRead{Dependencies: []dependencyEntry{}, Packages: []string{}}
	current := emptyDependency()
	for parser.NextExpression() {
		expression := parser.Expression()
		switch expression.Kind {
		case unstable.Table, unstable.ArrayTable:
			parsed = appendDependency(parsed, current)
			current = emptyDependency()
			table = metadataKeys(expression)
		case unstable.KeyValue:
			key := strings.Join(metadataKeys(expression), ".")
			switch {
			case len(table) == 0 && key == "packages" && expression.Value().Kind == unstable.Array:
				items := expression.Value().Children()
				for items.Next() {
					parsed = appendDependency(parsed, inlineDependency(items.Node()))
				}
			case metadataEntries(table):
				parsed = appendDependency(parsed, inlineDependency(expression.Value()))
			case metadataFields(table):
				current = readDependencyField(current, key, expression.Value())
			}
		case unstable.Invalid, unstable.Comment, unstable.Key, unstable.Array, unstable.InlineTable, unstable.String, unstable.Bool, unstable.Float, unstable.Integer, unstable.LocalDate, unstable.LocalTime, unstable.LocalDateTime, unstable.DateTime:
		}
	}
	parsed = appendDependency(parsed, current)
	return outcome.Fold(outcome.TupleToResult(parsed, parser.Error()), outcome.Ok[compilerMetadataRead], func(cause error) outcome.Result[compilerMetadataRead] {
		return outcome.Err[compilerMetadataRead](CompilerMetadataMalformed{Path: path, Content: content, Cause: cause})
	})
}

func metadataKeys(node *unstable.Node) []string {
	keys := []string{}
	iterator := node.Key()
	for iterator.Next() {
		keys = append(keys, string(iterator.Node().Data))
	}
	return keys
}

func metadataEntries(table []string) bool {
	return len(table) == 1 && slices.Contains([]string{"dependencies", "dev_dependencies", "dev-dependencies", "requirements"}, table[0])
}

func metadataFields(table []string) bool {
	return (len(table) == 2 && metadataEntries(table[:1])) || (len(table) == 1 && table[0] == "packages")
}

func inlineDependency(node *unstable.Node) dependencyEntry {
	dependency := emptyDependency()
	if node.Kind == unstable.InlineTable {
		fields := node.Children()
		for fields.Next() {
			field := fields.Node()
			dependency = readDependencyField(dependency, strings.Join(metadataKeys(field), "."), field.Value())
		}
	}
	return dependency
}

func readDependencyField(entry dependencyEntry, name string, node *unstable.Node) dependencyEntry {
	switch name {
	case "path":
		entry.Path = outcome.Some(dependencyPath{Text: string(node.Data), Range: node.Raw})
	case "git":
		entry.Git = string(node.Data)
	case "name":
		entry.Name = string(node.Data)
	case "source":
		entry.Source = string(node.Data)
	}
	return entry
}

func appendDependency(parsed compilerMetadataRead, entry dependencyEntry) compilerMetadataRead {
	if entry.Name != "" {
		parsed.Packages = append(parsed.Packages, entry.Name)
	}
	outcome.ForEachOption(entry.Path, func(dependencyPath) { parsed.Dependencies = append(parsed.Dependencies, entry) })
	return parsed
}
