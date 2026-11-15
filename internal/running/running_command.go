// Package running runs the nocontract command: generate writes the Go file that serves each
// package's routes and the Effect HttpApi of all of them, and check refuses any of those files
// that differs from what generate would write.
package running

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/creachadair/atomicfile"
	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/effect"
	"github.com/CheckPickerUpper/nocontract/go/reading"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/go/writing"
)

// DefaultProjectFile is the project file read when -project names none.
const DefaultProjectFile = "nocontract.json"

// Request is one run: the command's arguments and the context its package loading runs under.
type Request struct {
	Arguments []string
	Context   context.Context
}

// Project is a project file: the language its backend is written in, the package patterns
// holding its routes, the TypeScript file to write, relative to the project file, and the name of
// the Effect API.
type Project struct {
	Language   string   `json:"language"`
	Packages   []string `json:"packages"`
	TypeScript string   `json:"typescript"`
	API        string   `json:"api"`
}

// Outcome is what one run ended as.
//
//sumtype:decl
type Outcome interface {
	isOutcome()
	ExitCode() int
	Report() string
}

// FilesWritten is a generate that wrote or deleted the files it lists.
//
//ergo:not-an-error
type FilesWritten struct{ Paths []string }

// FilesCurrent is a run that found every generated file already as generate writes it.
//
//ergo:not-an-error
type FilesCurrent struct{ Paths []string }

// FilesStale is a check that found the listed files differing from what generate writes.
//
//ergo:code NC-1034
type FilesStale struct{ Paths []string }

// DeclarationsRefused is a run that refused declarations, listed in source order.
//
//ergo:code NC-1035
type DeclarationsRefused struct{ Refusals []reading.Refusal }

// UsageRefused is a run whose arguments name no command it runs.
//
//ergo:code NC-1036
type UsageRefused struct{ Reason string }

// ProjectUnreadable is a project file that could not be read or holds no usable project.
//
//ergo:code NC-1037
type ProjectUnreadable struct {
	Path   string
	Reason string
}

// PackagesNotLoaded is a project whose packages holding routes Go could not load and type-check.
//
//ergo:code NC-1038
type PackagesNotLoaded struct{ Problems []string }

// FileNotWritten is a generate that could not write or delete one file.
//
//ergo:code NC-1039
type FileNotWritten struct {
	Path   string
	Reason string
}

func (FilesWritten) isOutcome()        {}
func (FilesCurrent) isOutcome()        {}
func (FilesStale) isOutcome()          {}
func (DeclarationsRefused) isOutcome() {}
func (UsageRefused) isOutcome()        {}
func (ProjectUnreadable) isOutcome()   {}
func (PackagesNotLoaded) isOutcome()   {}
func (FileNotWritten) isOutcome()      {}

// ExitCode is 0: the files were written.
func (FilesWritten) ExitCode() int { return 0 }

// ExitCode is 0: the files are current.
func (FilesCurrent) ExitCode() int { return 0 }

// ExitCode is 1: the files must be generated again.
func (FilesStale) ExitCode() int { return 1 }

// ExitCode is 1: declarations must change.
func (DeclarationsRefused) ExitCode() int { return 1 }

// ExitCode is 2: the run could not start.
func (UsageRefused) ExitCode() int { return 2 }

// ExitCode is 2: the run could not start.
func (ProjectUnreadable) ExitCode() int { return 2 }

// ExitCode is 2: the run could not read the routes.
func (PackagesNotLoaded) ExitCode() int { return 2 }

// ExitCode is 2: the run could not write its result.
func (FileNotWritten) ExitCode() int { return 2 }

// Report lists the files written or deleted.
func (written FilesWritten) Report() string {
	return "wrote " + strings.Join(written.Paths, "\nwrote ")
}

// Report lists the current files.
func (current FilesCurrent) Report() string {
	return strings.Join(current.Paths, " is current\n") + " is current"
}

// Report lists the stale files and how to refresh them.
func (stale FilesStale) Report() string {
	return codeOf(stale) + " these files differ from what nocontract generate writes; run it again:\n" + strings.Join(stale.Paths, "\n")
}

// Report lists each refusal with its place and code.
func (refused DeclarationsRefused) Report() string {
	lines := []string{}
	for _, refusal := range refused.Refusals {
		lines = append(lines, fmt.Sprintf("%s: %s %s", refusal.Position, problemCode(refusal.Problem), refusal.Problem.Explain()))
	}
	return strings.Join(lines, "\n")
}

// Report says how to run the command.
func (refused UsageRefused) Report() string {
	return codeOf(refused) + " " + refused.Reason + "\nusage: nocontract generate|check [-project " + DefaultProjectFile + "]"
}

// Report names the project file and why it is unusable.
func (unreadable ProjectUnreadable) Report() string {
	return codeOf(unreadable) + " " + unreadable.Path + ": " + unreadable.Reason
}

// Report lists what Go could not load.
func (failed PackagesNotLoaded) Report() string {
	return codeOf(failed) + " the packages holding routes did not load:\n" + strings.Join(failed.Problems, "\n")
}

// Report names the file and why it was not written.
func (failed FileNotWritten) Report() string {
	return codeOf(failed) + " " + failed.Path + " was not written: " + failed.Reason
}

func codeOf(failure ergo.Failure) string { return failure.ErrorCode().String() }

func problemCode(problem reading.Problem) string {
	if coded, isCoded := problem.(ergo.Failure); isCoded {
		return coded.ErrorCode().String()
	}
	return ""
}

// Print writes the outcome's report to stream, with a closing newline.
func Print(outcome Outcome, stream io.Writer) mo.Result[int] {
	return mo.TupleToResult(fmt.Fprintln(stream, outcome.Report()))
}

// command is what the arguments ask for.
//
//sumtype:decl
type command interface{ isCommand() }

// generating writes the files.
//
//ergo:not-an-error
type generating struct{ projectFile string }

// checking compares the files.
//
//ergo:not-an-error
type checking struct{ projectFile string }

func (generating) isCommand()   {}
func (checking) isCommand()     {}
func (UsageRefused) isCommand() {}

func parseCommand(arguments []string) command {
	if len(arguments) == 0 {
		return UsageRefused{Reason: "no command was given"}
	}
	flags := flag.NewFlagSet(arguments[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	projectFile := flags.String("project", DefaultProjectFile, "the project file")
	return mo.Fold(mo.TupleToResult(struct{}{}, flags.Parse(arguments[1:])), func(struct{}) command {
		if flags.NArg() > 0 {
			return UsageRefused{Reason: "unexpected arguments: " + strings.Join(flags.Args(), " ")}
		}
		switch arguments[0] {
		case "generate":
			return generating{projectFile: *projectFile}
		case "check":
			return checking{projectFile: *projectFile}
		default:
			return UsageRefused{Reason: "unknown command " + arguments[0]}
		}
	}, func(cause error) command { return UsageRefused{Reason: cause.Error()} })
}

// Run runs the command the arguments name.
func Run(request Request) Outcome {
	switch parsed := parseCommand(request.Arguments).(type) {
	case generating:
		return absolute(parsed.projectFile, func(projectFile string) Outcome { return generated(request.Context, projectFile, writeFiles) })
	case checking:
		return absolute(parsed.projectFile, func(projectFile string) Outcome { return generated(request.Context, projectFile, compareFiles) })
	case UsageRefused:
		return parsed
	case nil:
	}
	return UsageRefused{Reason: "no command was given"}
}

// absolute runs next on the project file's absolute path, so every file the run names, found by
// walking the folder or by Go's loader, has one spelling.
func absolute(projectFile string, next func(string) Outcome) Outcome {
	return mo.Fold(mo.TupleToResult(filepath.Abs(projectFile)), next, func(cause error) Outcome {
		return ProjectUnreadable{Path: projectFile, Reason: cause.Error()}
	})
}

// generatedFile is one file generate owns: what it must hold, or that it must not exist.
type generatedFile struct {
	path    string
	content mo.Option[[]byte]
}

// generated reads the project, renders its files and hands them to finish.
func generated(ctx context.Context, projectFile string, finish func([]generatedFile) Outcome) Outcome {
	return mo.Fold(readProject(projectFile), func(project Project) Outcome {
		directory := filepath.Dir(projectFile)
		existing := existingGenerated(directory)
		return mo.Fold(loadPackages(ctx, directory, project.Packages, existing), func(loaded []*packages.Package) Outcome {
			switch read := reading.ReadRoutes(reading.Request{Packages: loaded, APIName: project.API}).(type) {
			case reading.RoutesRead:
				if problems := loadProblems(loaded, read.Read); len(problems) > 0 {
					return PackagesNotLoaded{Problems: problems}
				}
				return mo.Fold(renderedFiles(read, filepath.Join(directory, project.TypeScript), existing), finish,
					func(cause error) Outcome { return FileNotWritten{Path: directory, Reason: cause.Error()} })
			case reading.RoutesRefused:
				if problems := loadProblems(loaded, read.Read); len(problems) > 0 {
					return PackagesNotLoaded{Problems: problems}
				}
				return DeclarationsRefused{Refusals: read.Refusals}
			case nil:
			}
			return DeclarationsRefused{Refusals: []reading.Refusal{}}
		}, func(cause error) Outcome { return PackagesNotLoaded{Problems: []string{cause.Error()}} })
	}, func(cause error) Outcome { return ProjectUnreadable{Path: projectFile, Reason: cause.Error()} })
}

// renderedFiles is the TypeScript file, each package's Go file, and every generated Go file of a
// package that no longer declares a route, which must go.
func renderedFiles(read reading.RoutesRead, typeScript string, existing map[string]string) mo.Result[[]generatedFile] {
	return mo.Fold(goFiles(read.Plan.Packages), func(served []generatedFile) mo.Result[[]generatedFile] {
		files := append([]generatedFile{{path: typeScript, content: mo.Some(effect.Render(read.API))}}, served...)
		for path := range existing {
			if !slices.ContainsFunc(served, func(file generatedFile) bool { return file.path == path }) {
				files = append(files, generatedFile{path: path, content: mo.None[[]byte]()})
			}
		}
		slices.SortFunc(files, func(left, right generatedFile) int { return cmp.Compare(left.path, right.path) })
		return mo.Ok(files)
	}, mo.Err[[]generatedFile])
}

// goFiles is the generated Go file of each package, or why one could not be formatted.
func goFiles(served []serverplan.Package) mo.Result[[]generatedFile] {
	if len(served) == 0 {
		return mo.Ok([]generatedFile{})
	}
	return mo.Fold(writing.Render(served[0]), func(goFile []byte) mo.Result[[]generatedFile] {
		file := generatedFile{path: filepath.Join(served[0].Directory, reading.GeneratedFileName), content: mo.Some(goFile)}
		return mo.Fold(goFiles(served[1:]), func(rest []generatedFile) mo.Result[[]generatedFile] {
			return mo.Ok(append([]generatedFile{file}, rest...))
		}, mo.Err[[]generatedFile])
	}, mo.Err[[]generatedFile])
}

// ProjectIncomplete is a project file missing a value it needs, or naming a language NoContract
// has no reader for.
//
//ergo:code NC-1040
type ProjectIncomplete struct{ Missing string }

func (incomplete ProjectIncomplete) Error() string {
	return "the project file names no " + incomplete.Missing
}

func readProject(projectFile string) mo.Result[Project] {
	return mo.Fold(mo.TupleToResult(os.ReadFile(projectFile)), func(content []byte) mo.Result[Project] {
		project := Project{Language: "", Packages: []string{}, TypeScript: "", API: ""}
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		return mo.Fold(mo.EmptyableToOption(decoder.Decode(&project)), mo.Err[Project], func(error) mo.Result[Project] {
			switch {
			case project.Language != "go":
				return mo.Err[Project](ProjectIncomplete{Missing: "language NoContract reads: go"})
			case len(project.Packages) == 0:
				return mo.Err[Project](ProjectIncomplete{Missing: "packages"})
			case project.TypeScript == "":
				return mo.Err[Project](ProjectIncomplete{Missing: "typescript file"})
			case project.API == "":
				return mo.Err[Project](ProjectIncomplete{Missing: "api name"})
			}
			return mo.Ok(project)
		})
	}, mo.Err[Project])
}

// existingGenerated is every generated Go file under directory, by path, with its package's name.
func existingGenerated(directory string) map[string]string {
	found := map[string]string{}
	files := token.NewFileSet()
	visit := func(path string, entry fs.DirEntry) error {
		name := entry.Name()
		if entry.IsDir() && path != directory && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "vendor" || name == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() || name != reading.GeneratedFileName {
			return nil
		}
		mo.Fold(mo.TupleToResult(parser.ParseFile(files, path, nil, parser.PackageClauseOnly)), func(clause *ast.File) struct{} {
			found[path] = clause.Name.Name
			return struct{}{}
		}, func(error) struct{} { return struct{}{} })
		return nil
	}
	// A folder that cannot be walked holds no generated file this run can read, so it is skipped.
	return mo.Fold(mo.EmptyableToOption(filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		return mo.Fold(mo.EmptyableToOption(walkErr), func(error) error { return filepath.SkipDir }, func(error) error { return visit(path, entry) })
	})), func(error) map[string]string { return found }, func(error) map[string]string { return found })
}

// LoadFailed is a load Go's package driver refused outright.
//
//ergo:code NC-1041
type LoadFailed struct{ Reason string }

func (failed LoadFailed) Error() string { return failed.Reason }

// loadPackages loads the project's packages and nocontract's, each with its syntax and types, with
// each existing generated file read as its stub.
func loadPackages(ctx context.Context, directory string, patterns []string, existing map[string]string) mo.Result[[]*packages.Package] {
	overlay := map[string][]byte{}
	for path, name := range existing {
		overlay[path] = writing.Stub(name)
	}
	config := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Context: ctx, Logf: func(string, ...any) {}, Dir: directory, Env: os.Environ(), BuildFlags: []string{}, Fset: token.NewFileSet(),
		ParseFile: nil, Tests: false, Overlay: overlay,
	}
	return mo.TupleToResult(packages.Load(config, append(patterns, reading.NoContractPath)...))
}

// loadProblems is every problem Go reported in a package whose declarations were read.
func loadProblems(loaded []*packages.Package, read []string) []string {
	problems := []string{}
	for _, each := range loaded {
		if !slices.Contains(read, each.PkgPath) && each.PkgPath != reading.NoContractPath {
			continue
		}
		for _, problem := range each.Errors {
			problems = append(problems, problem.Error())
		}
	}
	return problems
}

func writeFiles(files []generatedFile) Outcome {
	return writeEach(files, files, []string{})
}

// writeEach writes or deletes each remaining file that is not current, stopping at the first
// that cannot be.
func writeEach(all, remaining []generatedFile, written []string) Outcome {
	if len(remaining) == 0 {
		if len(written) == 0 {
			return compareFiles(all)
		}
		return FilesWritten{Paths: written}
	}
	file := remaining[0]
	if _, isCurrent := compareFiles([]generatedFile{file}).(FilesCurrent); isCurrent {
		return writeEach(all, remaining[1:], written)
	}
	failure := mo.Fold(file.content, func(content []byte) error {
		return mo.Fold(mo.EmptyableToOption(os.MkdirAll(filepath.Dir(file.path), 0o755)), func(cause error) error { return cause },
			func(error) error { return atomicfile.WriteData(file.path, content, 0o644) })
	}, func(error) error { return os.Remove(file.path) })
	return mo.Fold(mo.EmptyableToOption(failure), func(cause error) Outcome {
		return FileNotWritten{Path: file.path, Reason: cause.Error()}
	}, func(error) Outcome { return writeEach(all, remaining[1:], append(written, file.path)) })
}

func compareFiles(files []generatedFile) Outcome {
	stale, current := []string{}, []string{}
	for _, file := range files {
		onDisk := mo.TupleToResult(os.ReadFile(file.path))
		matches := mo.Fold(file.content, func(content []byte) bool {
			return mo.Fold(onDisk, func(found []byte) bool { return bytes.Equal(found, content) }, func(error) bool { return false })
		}, func(error) bool {
			return mo.Fold(onDisk, func([]byte) bool { return false }, func(error) bool { return true })
		})
		if matches {
			current = append(current, file.path)
			continue
		}
		stale = append(stale, file.path)
	}
	if len(stale) > 0 {
		return FilesStale{Paths: stale}
	}
	return FilesCurrent{Paths: current}
}
