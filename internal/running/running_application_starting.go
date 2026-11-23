package running

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/internal/projectfile"
)

// ApplicationCommand is the executable and arguments the application owns.
type ApplicationCommand struct {
	Name      string
	Arguments []string
}

// ApplicationLaunch keeps the project, working directory, and resolved executable of an attempt.
type ApplicationLaunch struct {
	ProjectFile string
	Directory   string
	Command     ApplicationCommand
	Path        string
}

// StartedApplication is a launched process and the time spent starting it.
type StartedApplication struct {
	Launch        ApplicationLaunch
	ProcessID     int
	StartDuration time.Duration
}

// ApplicationNotStarted keeps the command the operating system refused to start.
//
//ergo:code NC-1161
//ergo:message The application could not start.
type ApplicationNotStarted struct {
	Launch   ApplicationLaunch
	Duration time.Duration
	Cause    error
}

// ApplicationNotFinished keeps the process whose terminal status was not successful.
//
//ergo:code NC-1162
//ergo:message The application did not finish successfully.
type ApplicationNotFinished struct {
	Application StartedApplication
	Duration    time.Duration
	Cause       error
}

// ApplicationFinished is a process that exited successfully.
//
//ergo:not-an-error
type ApplicationFinished struct {
	Application StartedApplication
	Duration    time.Duration
}

// ApplicationStopped is a process that handled its command context's interrupt and exited.
//
//ergo:not-an-error
type ApplicationStopped struct {
	Application StartedApplication
	Duration    time.Duration
	Cause       error
}

func (ApplicationNotStarted) isOutcome()  {}
func (ApplicationNotFinished) isOutcome() {}
func (ApplicationFinished) isOutcome()    {}
func (ApplicationStopped) isOutcome()     {}

// ExitCode is 2 because the requested application could not run.
func (ApplicationNotStarted) ExitCode() int { return 2 }

// ExitCode is 2 because the application failed while it ran.
func (ApplicationNotFinished) ExitCode() int { return 2 }

// ExitCode is 0 because the application finished successfully.
func (ApplicationFinished) ExitCode() int { return 0 }

// ExitCode is 0 because the application stopped when asked.
func (ApplicationStopped) ExitCode() int { return 0 }

// Lines identifies the project, command, directory, duration and original start failure.
func (failed ApplicationNotStarted) Lines() []string {
	return append(described(failed), fmt.Sprintf("%s (start took %s)", failed.Launch, failed.Duration))
}

// Lines identifies the process, duration and original terminal failure.
func (failed ApplicationNotFinished) Lines() []string {
	return append(described(failed), fmt.Sprintf("%s (ran for %s)", failed.Application, failed.Duration))
}

// Lines reports the application that completed and its measured running time.
func (finished ApplicationFinished) Lines() []string {
	return []string{fmt.Sprintf("Finished %s after %s", finished.Application, finished.Duration)}
}

// Lines reports the application that handled the interrupt and the original cancellation.
func (stopped ApplicationStopped) Lines() []string {
	return []string{fmt.Sprintf("Stopped %s after %s: %v", stopped.Application, stopped.Duration, stopped.Cause)}
}

// String names the exact arguments without losing spaces inside one argument.
func (command ApplicationCommand) String() string {
	return fmt.Sprintf("%q %q", command.Name, command.Arguments)
}

// String identifies the requested command and the executable and directory used to start it.
func (launch ApplicationLaunch) String() string {
	return fmt.Sprintf("%s (executable %q, directory %q, project %q)", launch.Command, launch.Path, launch.Directory, launch.ProjectFile)
}

// String identifies the started process and its measured startup time.
func (application StartedApplication) String() string {
	return fmt.Sprintf("%s (process %d, started in %s)", application.Launch, application.ProcessID, application.StartDuration)
}

//ergo:not-an-error
type projectServe struct {
	projectFile string
	application ApplicationCommand
}

func (projectServe) isCommand() {}

// MissingApplicationCommand is a serve invocation with no executable after its flags.
// It is reported by UsageRefused under NC-1036.
//
//ergo:not-an-error
type MissingApplicationCommand struct {
	Arguments []string
}

func (MissingApplicationCommand) isUsageProblem() {}

const serveUsage = "nocontract serve [-project " + DefaultProjectFile + "] -- <app command> [arguments]"

func parseServe(arguments []string) command {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	projectFile := flags.String("project", DefaultProjectFile, "the project file")
	return outcome.Fold(outcome.TupleToResult(struct{}{}, flags.Parse(arguments)), func(struct{}) command {
		if flags.NArg() == 0 || flags.Args()[0] == "" {
			return UsageRefused{Problem: MissingApplicationCommand{Arguments: slices.Clone(arguments)}, Usage: serveUsage}
		}
		return projectServe{projectFile: *projectFile, application: ApplicationCommand{Name: flags.Args()[0], Arguments: slices.Clone(flags.Args()[1:])}}
	}, func(cause error) command {
		return UsageRefused{Problem: FlagsUnread{Cause: cause}, Usage: serveUsage}
	})
}

func startApplication(lifetime context.Context, project projectfile.Project, requested ApplicationCommand) Outcome {
	command := exec.CommandContext(lifetime, requested.Name, requested.Arguments...)
	command.Dir = project.Directory()
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	configureApplicationProcess(command)
	launch := ApplicationLaunch{ProjectFile: project.File(), Directory: command.Dir, Command: requested, Path: command.Path}
	startedAt := time.Now()
	return outcome.Fold(outcome.TupleToResult(struct{}{}, command.Start()), func(struct{}) Outcome {
		application := StartedApplication{Launch: launch, ProcessID: command.Process.Pid, StartDuration: time.Since(startedAt)}
		waitingAt := time.Now()
		return outcome.Fold(outcome.TupleToResult(struct{}{}, command.Wait()), func(struct{}) Outcome {
			return ApplicationFinished{Application: application, Duration: time.Since(waitingAt)}
		}, func(cause error) Outcome {
			elapsed := time.Since(waitingAt)
			if errors.Is(cause, lifetime.Err()) {
				return outcome.FoldOption(outcome.EmptyableToOption(command.ProcessState), func(state *os.ProcessState) Outcome {
					if state.Success() {
						return ApplicationStopped{Application: application, Duration: elapsed, Cause: cause}
					}
					return ApplicationNotFinished{Application: application, Duration: elapsed, Cause: cause}
				}, func() Outcome {
					return ApplicationNotFinished{Application: application, Duration: elapsed, Cause: cause}
				})
			}
			return ApplicationNotFinished{Application: application, Duration: elapsed, Cause: cause}
		})
	}, func(cause error) Outcome {
		return ApplicationNotStarted{Launch: launch, Duration: time.Since(startedAt), Cause: cause}
	})
}
