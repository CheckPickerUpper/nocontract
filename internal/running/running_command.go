// Package running runs the nocontract command: generate writes the Go file that serves each
// package's routes and the Effect HttpApi of all of them, and check refuses any of those files
// that differs from what generate would write.
package running

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/token"
	"io"
	"io/fs"
	"maps"
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
	"github.com/CheckPickerUpper/nocontract/messages"
)

// DefaultProjectFile is the project file read when -project names none.
const DefaultProjectFile = "nocontract.json"

// Request is one run: the command's arguments and the context its package loading runs under.
type Request struct {
	Arguments []string
	Context   context.Context
}

// writtenProject is a project file as it is written: the language its backend is written in, the
// package patterns holding its routes, the TypeScript file to write, relative to the project file,
// and the name of the Effect API.
type writtenProject struct {
	Language   string   `json:"language"`
	Packages   []string `json:"packages"`
	TypeScript string   `json:"typescript"`
	API        string   `json:"api"`
}

// Project is a project file read: its backend, the package patterns holding its routes, the
// TypeScript file to write and the name of the Effect API.
type Project struct {
	Backend    Backend
	Packages   []string
	TypeScript string
	API        string
}

// Backend is the language a project's backend is written in.
//
//sumtype:decl
type Backend interface{ isBackend() }

// GoBackend is a backend written in Go.
//
//ergo:not-an-error
type GoBackend struct{}

func (GoBackend) isBackend() {}

// parseBackend is the language a project file's "language" names.
func parseBackend(language string) mo.Option[Backend] {
	if language == "go" {
		return mo.Some[Backend](GoBackend{})
	}
	return mo.None[Backend]()
}

// Outcome is what one run ended as.
//
//sumtype:decl
type Outcome interface {
	isOutcome()
	ExitCode() int
	Lines() []string
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
//ergo:message These files differ from what nocontract generate writes; run it again:
type FilesStale struct{ Paths []string }

// DeclarationsRefused is a run that refused declarations, listed in source order.
//
//ergo:code NC-1035
//ergo:message NoContract refused these declarations:
type DeclarationsRefused struct{ Refusals []reading.Refusal }

// UsageRefused is a run whose arguments name no command it runs: the problem with them, and how
// the command is run.
//
//ergo:code NC-1036
//ergo:message The command could not run: {Problem}.
type UsageRefused struct {
	Problem UsageProblem
	Usage   string
}

// UsageProblem is what is wrong with a run's arguments.
//
//sumtype:decl
type UsageProblem interface{ isUsageProblem() }

// NoCommand is a run given no arguments.
//
//ergo:code NC-1042
//ergo:message no command was given
type NoCommand struct{}

// UnknownCommand is a run naming a command NoContract does not run.
//
//ergo:code NC-1059
//ergo:message {Name} is not a command it runs
type UnknownCommand struct{ Name string }

// ExtraArguments is a run given arguments after its flags.
//
//ergo:code NC-1060
//ergo:message it was given arguments after its flags
type ExtraArguments struct{ Arguments []string }

// FlagsUnread is a run whose flags the flag package refused, with its error.
//
//ergo:code NC-1061
//ergo:message its flags could not be parsed
type FlagsUnread struct{ Cause error }

func (NoCommand) isUsageProblem()      {}
func (UnknownCommand) isUsageProblem() {}
func (ExtraArguments) isUsageProblem() {}
func (FlagsUnread) isUsageProblem()    {}

// ProjectUnreadable is a project file that could not be read or holds no usable project, with why.
//
//ergo:code NC-1037
//ergo:message The project file {Path} could not be used.
type ProjectUnreadable struct {
	Path  string
	Cause error
}

// PackagesNotLoaded is a project whose packages holding routes Go could not load and type-check.
//
//ergo:code NC-1038
//ergo:message Go could not load the packages holding routes.
type PackagesNotLoaded struct{ Errors []error }

// FileNotWritten is a generate that could not write or delete one file.
//
//ergo:code NC-1039
//ergo:message The file {Path} could not be written.
type FileNotWritten struct {
	Path  string
	Cause error
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

// Lines lists the files written or deleted.
func (written FilesWritten) Lines() []string {
	lines := []string{}
	for _, path := range written.Paths {
		lines = append(lines, "wrote "+path)
	}
	return lines
}

// Lines lists the current files.
func (current FilesCurrent) Lines() []string {
	lines := []string{}
	for _, path := range current.Paths {
		lines = append(lines, path+" is current")
	}
	return lines
}

// Lines is the failure, then each stale file.
func (stale FilesStale) Lines() []string {
	return append(described(stale), stale.Paths...)
}

// Lines is the failure, then each refusal with where it is written, its code and its message.
func (refused DeclarationsRefused) Lines() []string {
	lines := described(refused)
	for _, refusal := range refused.Refusals {
		// described's first line is always the code and message; the rest are causes.
		problem := described(refusal.Problem)
		lines = append(append(lines, refusal.Position.String()+": "+problem[0]), problem[1:]...)
	}
	return lines
}

// Lines is the failure, then how the command is run.
func (refused UsageRefused) Lines() []string {
	return append(described(refused), "usage: "+refused.Usage)
}

// Lines is the failure and its cause.
func (unreadable ProjectUnreadable) Lines() []string { return described(unreadable) }

// Lines is the failure and what Go reported.
func (failed PackagesNotLoaded) Lines() []string { return described(failed) }

// Lines is the failure and its cause.
func (failed FileNotWritten) Lines() []string { return described(failed) }

// describedFailure is a failure with the methods ErGo generates for it.
type describedFailure interface {
	ErrorCode() ergo.Code
	Report(language messages.Language) ergo.Report
	Causes() []error
}

// described is a failure as ErGo's own command prints one: its code and its message in the source
// language, then one cause line for each line of each Go error it holds.
func described(failure describedFailure) []string {
	lines := []string{failure.ErrorCode().String() + " " + failure.Report(messages.Match("")).Message}
	for _, cause := range failure.Causes() {
		for line := range strings.SplitSeq(cause.Error(), "\n") {
			lines = append(lines, "  cause: "+line)
		}
	}
	return lines
}

// Print writes the outcome's lines to stream, each with a closing newline.
func Print(outcome Outcome, stream io.Writer) mo.Result[int] {
	return mo.TupleToResult(fmt.Fprintln(stream, strings.Join(outcome.Lines(), "\n")))
}

// Finish reports a command's outcome, to standard output when it succeeded and standard error when
// it did not, and is the exit code the command ends with: 2 when the report could not be written.
func Finish(outcome Outcome) int {
	stream := os.Stdout
	if outcome.ExitCode() != 0 {
		stream = os.Stderr
	}
	return mo.Fold(Print(outcome, stream), func(int) int { return outcome.ExitCode() }, func(error) int { return 2 })
}

// command is what the arguments ask for.
//
//sumtype:decl
type command interface{ isCommand() }

// projectRun is a run over one project file, doing what its verb asks with the rendered files.
//
//ergo:not-an-error
type projectRun struct {
	projectFile string
	finish      finisher
}

func (projectRun) isCommand()   {}
func (UsageRefused) isCommand() {}

// finisher is what a run does with the files it rendered: generate writes them, check compares
// them.
type finisher func([]generatedFile) Outcome

// verbParse is a command line read as its verb, or refused.
//
//sumtype:decl
type verbParse interface{ isVerbParse() }

// verbRead is a command line whose verb and flags were read: what the verb does with the files.
//
//ergo:not-an-error
type verbRead struct{ finish finisher }

// verbRefused is a command line that names nothing to run, and what is wrong with it.
//
//ergo:not-an-error
type verbRefused struct{ problem UsageProblem }

func (verbRead) isVerbParse()    {}
func (verbRefused) isVerbParse() {}

// parseVerb reads the first argument as generate or check and the rest as the flags declared on
// flags, which the caller reads once the line is read.
func parseVerb(arguments []string, flags *flag.FlagSet) verbParse {
	if len(arguments) == 0 {
		return verbRefused{problem: NoCommand{}}
	}
	flags.SetOutput(io.Discard)
	return mo.Fold(mo.TupleToResult(struct{}{}, flags.Parse(arguments[1:])), func(struct{}) verbParse {
		if flags.NArg() > 0 {
			return verbRefused{problem: ExtraArguments{Arguments: flags.Args()}}
		}
		switch arguments[0] {
		case "generate":
			return verbRead{finish: writeFiles}
		case "check":
			return verbRead{finish: compareFiles}
		}
		return verbRefused{problem: UnknownCommand{Name: arguments[0]}}
	}, func(cause error) verbParse { return verbRefused{problem: FlagsUnread{Cause: cause}} })
}

// commandUsage is how the nocontract command is run.
const commandUsage = "nocontract generate|check [-project " + DefaultProjectFile + "]"

func parseCommand(arguments []string) command {
	flags := flag.NewFlagSet("nocontract", flag.ContinueOnError)
	projectFile := flags.String("project", DefaultProjectFile, "the project file")
	switch parsed := parseVerb(arguments, flags).(type) {
	case verbRead:
		return projectRun{projectFile: *projectFile, finish: parsed.finish}
	case verbRefused:
		return UsageRefused{Problem: parsed.problem, Usage: commandUsage}
	case nil:
	}
	return UsageRefused{Problem: NoCommand{}, Usage: commandUsage}
}

// Run runs the command the arguments name.
func Run(request Request) Outcome {
	switch parsed := parseCommand(request.Arguments).(type) {
	case projectRun:
		return absolute(parsed.projectFile, func(projectFile string) Outcome { return generated(request.Context, projectFile, parsed.finish) })
	case UsageRefused:
		return parsed
	case nil:
	}
	return UsageRefused{Problem: NoCommand{}, Usage: commandUsage}
}

// absolute runs next on the project file's absolute path, so every file the run names, found by
// walking the folder or by Go's loader, has one spelling.
func absolute(projectFile string, next func(string) Outcome) Outcome {
	return mo.Fold(mo.TupleToResult(filepath.Abs(projectFile)), next, func(cause error) Outcome {
		return ProjectUnreadable{Path: projectFile, Cause: cause}
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
		switch project.Backend.(type) {
		case GoBackend:
			return generatedGo(ctx, projectFile, project, finish)
		case nil:
		}
		return ProjectUnreadable{Path: projectFile, Cause: LanguageUnknown{Language: ""}}
	}, func(cause error) Outcome { return ProjectUnreadable{Path: projectFile, Cause: cause} })
}

// generatedGo reads a Go backend's routes, renders its files and hands them to finish.
func generatedGo(ctx context.Context, projectFile string, project Project, finish func([]generatedFile) Outcome) Outcome {
	directory := filepath.Dir(projectFile)
	existing := existingGenerated(directory)
	return mo.Fold(loadPackages(ctx, directory, project.Packages, existing), func(loaded []*packages.Package) Outcome {
		read := reading.ReadRoutes(reading.Request{Packages: loaded, APIName: project.API})
		if problems := loadProblems(loaded, read.Read); len(problems) > 0 {
			return PackagesNotLoaded{Errors: problems}
		}
		switch routes := read.Routes.(type) {
		case reading.RoutesRead:
			return mo.Fold(renderedFiles(routes, filepath.Join(directory, project.TypeScript), existing), finish,
				func(cause error) Outcome { return FileNotWritten{Path: directory, Cause: cause} })
		case reading.RoutesRefused:
			return DeclarationsRefused{Refusals: routes.Refusals}
		case nil:
		}
		return DeclarationsRefused{Refusals: []reading.Refusal{}}
	}, func(cause error) Outcome { return PackagesNotLoaded{Errors: []error{cause}} })
}

// renderedFiles is the TypeScript file, each package's Go file, and every generated Go file of a
// package that no longer declares a route, which must go.
func renderedFiles(read reading.RoutesRead, typeScript string, existing map[string][]byte) mo.Result[[]generatedFile] {
	return mo.Fold(goFiles(read.Plan.Packages), func(served []generatedFile) mo.Result[[]generatedFile] {
		// Every existing generated file goes unless this run writes it again.
		contents := map[string]mo.Option[[]byte]{typeScript: mo.Some(effect.Render(read.API))}
		for path := range existing {
			contents[path] = mo.None[[]byte]()
		}
		for _, file := range served {
			contents[file.path] = file.content
		}
		files := []generatedFile{}
		for _, path := range slices.Sorted(maps.Keys(contents)) {
			files = append(files, generatedFile{path: path, content: contents[path]})
		}
		return mo.Ok(files)
	}, mo.Err[[]generatedFile])
}

// goFiles is the generated Go file of each package, or why one could not be formatted.
func goFiles(served []serverplan.Package) mo.Result[[]generatedFile] {
	files := mo.Ok([]generatedFile{})
	for _, each := range served {
		files = mo.Fold(files, func(done []generatedFile) mo.Result[[]generatedFile] {
			return mo.Fold(writing.Render(each), func(goFile []byte) mo.Result[[]generatedFile] {
				return mo.Ok(append(slices.Clone(done), generatedFile{path: filepath.Join(each.Directory, serverplan.FileName), content: mo.Some(goFile)}))
			}, mo.Err[[]generatedFile])
		}, mo.Err[[]generatedFile])
	}
	return files
}

// PackagesMissing is a project file naming no package patterns.
//
//ergo:code NC-1040
//ergo:message The project file names no packages.
type PackagesMissing struct{}

// Error is the failure's message in the source language.
func (missing PackagesMissing) Error() string { return missing.Report(messages.Match("")).Message }

// TypeScriptMissing is a project file naming no TypeScript file to write.
//
//ergo:code NC-1053
//ergo:message The project file names no typescript file.
type TypeScriptMissing struct{}

// Error is the failure's message in the source language.
func (missing TypeScriptMissing) Error() string { return missing.Report(messages.Match("")).Message }

// APINameMissing is a project file naming no API class.
//
//ergo:code NC-1054
//ergo:message The project file names no api.
type APINameMissing struct{}

// Error is the failure's message in the source language.
func (missing APINameMissing) Error() string { return missing.Report(messages.Match("")).Message }

// LanguageUnknown is a project file naming a language NoContract reads no backend in.
//
//ergo:code NC-1055
//ergo:message The project file names the language "{Language}"; NoContract reads go.
type LanguageUnknown struct{ Language string }

// Error is the failure's message in the source language.
func (unknown LanguageUnknown) Error() string { return unknown.Report(messages.Match("")).Message }

func readProject(projectFile string) mo.Result[Project] {
	return mo.Fold(mo.TupleToResult(os.ReadFile(projectFile)), func(content []byte) mo.Result[Project] {
		written := writtenProject{Language: "", Packages: []string{}, TypeScript: "", API: ""}
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		return mo.Fold(mo.EmptyableToOption(decoder.Decode(&written)), mo.Err[Project], func(error) mo.Result[Project] {
			switch {
			case len(written.Packages) == 0:
				return mo.Err[Project](PackagesMissing{})
			case written.TypeScript == "":
				return mo.Err[Project](TypeScriptMissing{})
			case written.API == "":
				return mo.Err[Project](APINameMissing{})
			}
			return mo.Fold(parseBackend(written.Language), func(backend Backend) mo.Result[Project] {
				return mo.Ok(Project{Backend: backend, Packages: written.Packages, TypeScript: written.TypeScript, API: written.API})
			}, func(error) mo.Result[Project] { return mo.Err[Project](LanguageUnknown{Language: written.Language}) })
		})
	}, mo.Err[Project])
}

// existingGenerated is every generated Go file under directory, by path, with its content.
func existingGenerated(directory string) map[string][]byte {
	found := map[string][]byte{}
	visit := func(path string, entry fs.DirEntry) error {
		name := entry.Name()
		if entry.IsDir() && path != directory && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "vendor" || name == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		switch serverplan.SourceFileAt(path).(type) {
		case serverplan.GeneratedFile:
			return mo.Fold(mo.TupleToResult(os.ReadFile(path)), func(content []byte) error {
				found[path] = content
				return nil
			}, func(cause error) error { return cause })
		case serverplan.WrittenFile, nil:
		}
		return nil
	}
	// A folder that cannot be walked holds no generated file this run can read, so it is skipped.
	return mo.Fold(mo.EmptyableToOption(filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		return mo.Fold(mo.EmptyableToOption(walkErr), func(error) error { return filepath.SkipDir }, func(error) error { return visit(path, entry) })
	})), func(error) map[string][]byte { return found }, func(error) map[string][]byte { return found })
}

// loadPackages loads the project's packages and nocontract's, each with its syntax and types, with
// each existing generated file read as its stub.
func loadPackages(ctx context.Context, directory string, patterns []string, existing map[string][]byte) mo.Result[[]*packages.Package] {
	overlay := map[string][]byte{}
	for path, content := range existing {
		// A file too broken to stub is read as it is; its own errors are set aside like a stub's.
		overlay[path] = mo.Fold(writing.StubOf(content), func(stub []byte) []byte { return stub }, func(error) []byte { return content })
	}
	config := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Context: ctx, Logf: func(string, ...any) {}, Dir: directory, Env: os.Environ(), BuildFlags: []string{}, Fset: token.NewFileSet(),
		ParseFile: nil, Tests: false, Overlay: overlay,
	}
	return mo.TupleToResult(packages.Load(config, append(patterns, serverplan.NoContractPath)...))
}

// loadProblems is every problem Go reported in a package whose declarations were read, except a
// type error inside a generated file, which is read as its stub and written again by this run.
func loadProblems(loaded []*packages.Package, read []string) []error {
	problems := []error{}
	for _, each := range loaded {
		if !slices.Contains(read, each.PkgPath) {
			continue
		}
		for _, problem := range each.Errors {
			if problem.Kind != packages.TypeError {
				problems = append(problems, problem)
			}
		}
		for _, problem := range each.TypeErrors {
			switch serverplan.SourceFileAt(problem.Fset.Position(problem.Pos).Filename).(type) {
			case serverplan.WrittenFile:
				problems = append(problems, problem)
			case serverplan.GeneratedFile, nil:
			}
		}
	}
	return problems
}

func writeFiles(files []generatedFile) Outcome {
	return writeEach(writeProgress{all: files, remaining: files, written: []string{}})
}

// writeProgress is a generate's progress through its files: every file, those not yet written,
// and the paths written or deleted so far.
type writeProgress struct {
	all       []generatedFile
	remaining []generatedFile
	written   []string
}

// next is the progress past the first remaining file, having written it or not.
func (progress writeProgress) next(written []string) writeProgress {
	return writeProgress{all: progress.all, remaining: progress.remaining[1:], written: written}
}

// writeEach writes or deletes each remaining file that is not current, stopping at the first
// that cannot be.
func writeEach(progress writeProgress) Outcome {
	if len(progress.remaining) == 0 {
		if len(progress.written) == 0 {
			return compareFiles(progress.all)
		}
		return FilesWritten{Paths: progress.written}
	}
	file := progress.remaining[0]
	if _, isCurrent := compareFiles([]generatedFile{file}).(FilesCurrent); isCurrent {
		return writeEach(progress.next(progress.written))
	}
	failure := mo.Fold(file.content, func(content []byte) error {
		return mo.Fold(mo.EmptyableToOption(os.MkdirAll(filepath.Dir(file.path), 0o755)), func(cause error) error { return cause },
			func(error) error { return atomicfile.WriteData(file.path, content, 0o644) })
	}, func(error) error { return os.Remove(file.path) })
	return mo.Fold(mo.EmptyableToOption(failure), func(cause error) Outcome {
		return FileNotWritten{Path: file.path, Cause: cause}
	}, func(error) Outcome { return writeEach(progress.next(append(progress.written, file.path))) })
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
