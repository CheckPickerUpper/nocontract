// Package running runs the nocontract command: generate writes the Go file that serves each
// package's routes and the Effect HttpApi of all of them, and check refuses any of those files
// that differs from what generate would write.
package running

import (
	"bytes"
	"context"
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
	"github.com/CheckPickerUpper/outcome"
	"github.com/creachadair/atomicfile"
	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/effect"
	"github.com/CheckPickerUpper/nocontract/go/reading"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/go/writing"
	"github.com/CheckPickerUpper/nocontract/internal/projectfile"
	"github.com/CheckPickerUpper/nocontract/messages"
	"github.com/CheckPickerUpper/nocontract/openapi"
)

// DefaultProjectFile is the project file read when -project names none.
const DefaultProjectFile = "nocontract.json"

// Request is one run: the command's arguments and the context its package loading runs under.
type Request struct {
	Arguments []string
	Context   context.Context
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
type DeclarationsRefused struct{ Refusals []assembling.Refusal }

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

// ProjectFileRefused is a project file read whose content describes no project, with each place it
// is wrong, in file order.
//
//ergo:code NC-1089
//ergo:message The project file {Path} describes no project:
type ProjectFileRefused struct {
	Path     string
	Refusals []assembling.Refusal
}

// ProjectUnreadable is a project file whose path could not be resolved or that could not be read,
// with the operating system's error.
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
func (ProjectFileRefused) isOutcome()  {}
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

// ExitCode is 2: the run could not start.
func (ProjectFileRefused) ExitCode() int { return 2 }

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
	return placedRefusals(described(refused), refused.Refusals)
}

// Lines is the failure, then each place the project file is wrong, its code and its message.
func (refused ProjectFileRefused) Lines() []string {
	return placedRefusals(described(refused), refused.Refusals)
}

// placedRefusals is lines, then each refusal with where it is written, its code and its message.
func placedRefusals(lines []string, refusals []assembling.Refusal) []string {
	for _, refusal := range refusals {
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
	return projectfile.Read(projectFile, func(project projectfile.Project) Outcome {
		// Go is the only backend NoContract reads.
		switch project.Backend().(type) {
		case projectfile.GoBackend, nil:
		}
		return generatedGo(ctx, project, finish)
	}, func(cause error) Outcome {
		return ProjectUnreadable{Path: projectFile, Cause: cause}
	}, func(refusals []assembling.Refusal) Outcome {
		return ProjectFileRefused{Path: projectFile, Refusals: refusals}
	})
}

// generatedGo reads a Go backend's routes, renders its files and hands them to finish.
func generatedGo(ctx context.Context, project projectfile.Project, finish func([]generatedFile) Outcome) Outcome {
	directory := project.Directory()
	existing := existingGenerated(directory)
	return outcome.Fold(loadPackages(ctx, directory, project.Packages(), existing), func(loaded []*packages.Package) Outcome {
		read := reading.ReadRoutes(reading.Request{Packages: loaded, API: project.API(), Server: project.ServerFolder()})
		if problems := loadProblems(loaded, read.Read); len(problems) > 0 {
			return PackagesNotLoaded{Errors: problems}
		}
		switch assembled := assembling.Assemble(assembling.Request{Declared: read.Declared, Refused: read.Refused}).(type) {
		case assembling.APIAssembled:
			return outcome.Fold(renderedFiles(assembled.API, read.Plan, project, existing), func(files []generatedFile) Outcome {
				return judgedServing(ctx, project, files, finish)
			}, func(cause error) Outcome { return FileNotWritten{Path: directory, Cause: cause} })
		case assembling.APIRefused:
			return DeclarationsRefused{Refusals: assembled.Refusals}
		case nil:
		}
		return DeclarationsRefused{Refusals: []assembling.Refusal{}}
	}, func(cause error) Outcome { return PackagesNotLoaded{Errors: []error{cause}} })
}

// renderedFiles is the TypeScript file, the OpenAPI file, each package's Go file, the server file,
// the test of them all, and every generated Go file this run no longer writes, which must go.
func renderedFiles(api description.API, plan serverplan.Plan, project projectfile.Project, existing map[string][]byte) outcome.Result[[]generatedFile] {
	return outcome.Fold(servedFiles(plan, project.File()), func(served []generatedFile) outcome.Result[[]generatedFile] {
		return outcome.Fold(openapi.Render(api, project.OpenAPI().Version()), func(document []byte) outcome.Result[[]generatedFile] {
			// Every existing generated file goes unless this run writes it again.
			contents := map[string]mo.Option[[]byte]{project.TypeScriptFile(): mo.Some(effect.Render(api)), project.OpenAPI().File(): mo.Some(document)}
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
			return outcome.Ok(files)
		}, outcome.Err[[]generatedFile])
	}, outcome.Err[[]generatedFile])
}

// goFiles is the generated Go file of each package, or why one could not be formatted.
func goFiles(served []serverplan.Package) outcome.Result[[]generatedFile] {
	files := outcome.Ok([]generatedFile{})
	for _, each := range served {
		files = outcome.Fold(files, func(done []generatedFile) outcome.Result[[]generatedFile] {
			return outcome.Fold(writing.Render(each), func(goFile []byte) outcome.Result[[]generatedFile] {
				return outcome.Ok(append(slices.Clone(done), generatedFile{path: filepath.Join(each.Directory, serverplan.RoutesFileName), content: mo.Some(goFile)}))
			}, outcome.Err[[]generatedFile])
		}, outcome.Err[[]generatedFile])
	}
	return files
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

// setAsideFile is what the loader reads in place of every existing generated file: a file no build
// includes. What NoContract wrote earlier, against whatever runtime, never decides whether the
// project can be read, and no declaration is read from it.
const setAsideFile = "//go:build ignore\n\npackage ignored\n"

// loadPackages loads the project's packages and nocontract's, each with its syntax and types, with
// every existing generated file left out of the build.
func loadPackages(ctx context.Context, directory string, patterns []string, existing map[string][]byte) outcome.Result[[]*packages.Package] {
	overlay := map[string][]byte{}
	for path := range existing {
		overlay[path] = []byte(setAsideFile)
	}
	config := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Context: ctx, Logf: func(string, ...any) {}, Dir: directory, Env: os.Environ(), BuildFlags: []string{}, Fset: token.NewFileSet(),
		ParseFile: nil, Tests: false, Overlay: overlay,
	}
	return outcome.TupleToResult(packages.Load(config, append(patterns, serverplan.NoContractPath)...))
}

// judgedServing hands the files to finish when no file the project wrote serves anything but the
// generated API or makes a handler of its own. The project's packages are loaded again with their
// tests and with each Go file generate writes as it writes it, so the API they serve is the one
// these files declare.
func judgedServing(ctx context.Context, project projectfile.Project, files []generatedFile, finish func([]generatedFile) Outcome) Outcome {
	overlay := map[string][]byte{}
	for _, file := range files {
		if filepath.Ext(file.path) == ".go" {
			overlay[file.path] = mo.Fold(file.content, func(content []byte) []byte { return content }, func(error) []byte { return []byte(setAsideFile) })
		}
	}
	config := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Context: ctx, Logf: func(string, ...any) {}, Dir: project.Directory(), Env: os.Environ(), BuildFlags: []string{}, Fset: token.NewFileSet(),
		ParseFile: nil, Tests: true, Overlay: overlay,
	}
	return outcome.Fold(outcome.TupleToResult(packages.Load(config, project.Packages()...)), func(loaded []*packages.Package) Outcome {
		if problems := builtProblems(loaded); len(problems) > 0 {
			return PackagesNotLoaded{Errors: problems}
		}
		if refusals := reading.JudgeServing(reading.ServingRequest{Packages: loaded, Server: project.ServerFolder()}); len(refusals) > 0 {
			return DeclarationsRefused{Refusals: refusals}
		}
		return finish(files)
	}, func(cause error) Outcome { return PackagesNotLoaded{Errors: []error{cause}} })
}

// builtProblems is every problem Go reported in the project's packages built with generate's
// files, each once though a package and the same package with its tests both report it.
func builtProblems(loaded []*packages.Package) []error {
	problems, reported := []error{}, map[packages.Error]struct{}{}
	for _, each := range loaded {
		for _, problem := range each.Errors {
			if _, isReported := reported[problem]; !isReported {
				reported[problem] = struct{}{}
				problems = append(problems, problem)
			}
		}
	}
	return problems
}

// loadProblems is every problem Go reported in a package whose declarations were read. No
// generated file is built, so each problem is in a file the project wrote, and a package NoContract
// reads never names a declaration NoContract writes.
func loadProblems(loaded []*packages.Package, read []string) []error {
	problems := []error{}
	for _, each := range loaded {
		if !slices.Contains(read, each.PkgPath) {
			continue
		}
		for _, problem := range each.Errors {
			problems = append(problems, problem)
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
