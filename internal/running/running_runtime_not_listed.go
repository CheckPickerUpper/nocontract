package running

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/messages"
)

// RuntimeNotListed is a project load that listed no NoContract runtime package, with the patterns
// it listed and what go list answers when NoContract runs it again there. go/packages reads a
// project's dependencies from export data, and in that mode a go list that exits with an error,
// such as for a go.mod that needs updating, comes back as no package and no error.
//
//ergo:code NC-1130
//ergo:message Go listed no NoContract runtime package for the project in {Directory}. {Listing}
type RuntimeNotListed struct {
	Directory string
	Patterns  []string
	Listing   GoListAnswer
}

// Error is the failure's message in the source language.
func (missing RuntimeNotListed) Error() string { return missing.Report(messages.Match("")).Message }

// GoListAnswer is what go list answers when NoContract runs it again in the folder, with the
// environment and the patterns, of a load that listed nothing.
//
//sumtype:decl
type GoListAnswer interface{ isGoListAnswer() }

// GoListFailed is a go list that exited with a status other than 0, and what it wrote to its
// standard error.
//
//ergo:code NC-1131
//ergo:message Run there, go list exits with status {Status}: {Stderr}
type GoListFailed struct {
	Status int
	Stderr string
}

// GoListStopped is a go list a signal stopped before it exited, with the wait error naming the
// signal and what it wrote to its standard error.
//
//ergo:code NC-1132
//ergo:message Run there, go list was stopped before it exited.
type GoListStopped struct {
	Cause  error
	Stderr string
}

// GoListNotStarted is a go list the operating system could not start, with its error.
//
//ergo:code NC-1133
//ergo:message Run there, go list could not be started.
type GoListNotStarted struct{ Cause error }

// GoListSucceeded is a go list that exited with status 0, and what it wrote to its standard
// error: what stopped the load no longer stops go list.
//
//ergo:code NC-1134
//ergo:message Run there, go list exits with status 0.
type GoListSucceeded struct{ Stderr string }

func (GoListFailed) isGoListAnswer()     {}
func (GoListStopped) isGoListAnswer()    {}
func (GoListNotStarted) isGoListAnswer() {}
func (GoListSucceeded) isGoListAnswer()  {}

// runtimeNotListed is the failure of a load with config that listed no NoContract runtime package
// for patterns, holding what go list answers in the load's folder and environment. The overlay that
// sets NoContract's files aside is not given to go list, which reads no file's declarations.
func runtimeNotListed(ctx context.Context, config *packages.Config, patterns []string) RuntimeNotListed {
	stderr := &strings.Builder{}
	command := exec.CommandContext(ctx, "go", append([]string{"list", "-e", "--"}, patterns...)...)
	command.Dir, command.Env, command.Stdout, command.Stderr = config.Dir, config.Env, io.Discard, stderr
	ran := command.Run()
	return RuntimeNotListed{Directory: config.Dir, Patterns: patterns, Listing: goListAnswer(ran, strings.TrimSpace(stderr.String()))}
}

// goListAnswer is what a go list answered, from the error its run returned and what it wrote to its
// standard error.
func goListAnswer(ran error, stderr string) GoListAnswer {
	return outcome.FoldOption(outcome.EmptyableToOption(ran), func(cause error) GoListAnswer {
		ended, hasEnded := errors.AsType[*exec.ExitError](cause)
		if !hasEnded {
			return GoListNotStarted{Cause: cause}
		}
		if !ended.Exited() {
			return GoListStopped{Cause: cause, Stderr: stderr}
		}
		return GoListFailed{Status: ended.ExitCode(), Stderr: stderr}
	}, func() GoListAnswer { return GoListSucceeded{Stderr: stderr} })
}
