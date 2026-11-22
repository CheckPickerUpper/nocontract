package running

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/assembling"
	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/nocontract/internal/projectfile"
)

// NativeServer is the checked project's HTTP handler and the lifetime of its native dispatcher.
type NativeServer struct {
	Handler http.Handler
	Close   func() error
}

type projectRunFailed struct{ failed Outcome }

func (failed projectRunFailed) Error() string { return strings.Join(failed.failed.Lines(), "\n") }

// OpenNativeServer checks the server bindings and starts their native dispatcher.
func OpenNativeServer(context context.Context, projectFile string, layers []nocontract.Layer) outcome.Result[NativeServer] {
	return outcome.Fold(outcome.TupleToResult(filepath.Abs(projectFile)), func(path string) outcome.Result[NativeServer] {
		refused := func(failed Outcome) outcome.Result[NativeServer] {
			return outcome.Err[NativeServer](projectRunFailed{failed: failed})
		}
		return projectfile.Read(path, func(project projectfile.Project) outcome.Result[NativeServer] {
			return compileNative(context, project, func(built compiledNative) outcome.Result[NativeServer] {
				checked := onDisk(built.files, compareFiles)
				if checked.ExitCode() != 0 {
					return refused(checked)
				}
				switch plan := built.execution.(type) {
				case native.OdinDispatcher:
					return outcome.Fold(native.Start(context, plan.Command), func(dispatcher native.Dispatcher) outcome.Result[NativeServer] {
						return outcome.Ok(NativeServer{Handler: native.Handler(built.checked, plan.Guards, dispatcher, layers), Close: dispatcher.Close})
					}, outcome.Err[NativeServer])
				case native.GleamListener:
					return outcome.Err[NativeServer](GleamApplicationOwnsListener{Directory: plan.Directory})
				case nil:
				}
				return outcome.Err[NativeServer](nativeContractMissing{Directory: project.Directory()})
			}, refused)
		}, func(cause error) outcome.Result[NativeServer] {
			return refused(ProjectUnreadable{Path: path, Cause: cause})
		}, func(refusals []assembling.Refusal) outcome.Result[NativeServer] {
			return refused(ProjectFileRefused{Path: path, Refusals: refusals})
		})
	}, outcome.Err[NativeServer])
}

// GleamApplicationOwnsListener is a Go serving command directed at a BEAM application.
//
//ergo:code NC-1150
//ergo:message The Gleam application in {Directory} starts its own generated NoContract server.
type GleamApplicationOwnsListener struct{ Directory string }

func (refused GleamApplicationOwnsListener) Error() string {
	return fmt.Sprintf("Gleam starts its generated NoContract server in the application at %s; call the generated server.start function", refused.Directory)
}

//ergo:not-an-error
type projectServe struct {
	projectFile string
	address     string
}

func (projectServe) isCommand() {}

const serveUsage = "nocontract serve [-project " + DefaultProjectFile + "] [-address 127.0.0.1:8080]"

func parseServe(arguments []string) command {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	projectFile := flags.String("project", DefaultProjectFile, "the native project file")
	address := flags.String("address", "127.0.0.1:8080", "the HTTP listen address")
	return outcome.Fold(outcome.TupleToResult(struct{}{}, flags.Parse(arguments)), func(struct{}) command {
		if flags.NArg() > 0 {
			return UsageRefused{Problem: ExtraArguments{Arguments: flags.Args()}, Usage: serveUsage}
		}
		return projectServe{projectFile: *projectFile, address: *address}
	}, func(cause error) command {
		return UsageRefused{Problem: FlagsUnread{Cause: cause}, Usage: serveUsage}
	})
}

// NativeServerStopped is a listening server stopped with its command context.
//
//ergo:not-an-error
type NativeServerStopped struct{ Address string }

func (NativeServerStopped) isOutcome() {}

// ExitCode is 0 because the server stopped when asked.
func (NativeServerStopped) ExitCode() int { return 0 }

// Lines identifies the server that stopped.
func (stopped NativeServerStopped) Lines() []string {
	return []string{"Stopped serving " + stopped.Address}
}

func serveNative(lifetime context.Context, command projectServe) Outcome {
	fail := func(cause error) Outcome {
		return outcome.FoldOption(outcome.EmptyableToOption(cause), func(cause error) Outcome {
			return outcome.FoldOption(outcome.TupleToOption(errors.AsType[projectRunFailed](cause)), func(failed projectRunFailed) Outcome {
				return failed.failed
			}, func() Outcome { return NativeBackendFailed{Path: command.projectFile, Cause: cause} })
		}, func() Outcome { return NativeServerStopped{Address: command.address} })
	}
	return outcome.Fold(outcome.TupleToResult(net.Listen("tcp", command.address)), func(listener net.Listener) Outcome {
		return outcome.Fold(OpenNativeServer(lifetime, command.projectFile, []nocontract.Layer{}), func(server NativeServer) Outcome {
			served := serveNativeListener(lifetime, listener, server.Handler)
			stopped := server.Close()
			if errors.Is(lifetime.Err(), context.Canceled) && errors.Is(served, http.ErrServerClosed) {
				return NativeServerStopped{Address: listener.Addr().String()}
			}
			return fail(errors.Join(served, stopped))
		}, func(cause error) Outcome {
			return fail(errors.Join(cause, listener.Close()))
		})
	}, fail)
}

func serveNativeListener(lifetime context.Context, listener net.Listener, handler http.Handler) error {
	server := &http.Server{
		Addr: listener.Addr().String(), Handler: handler, DisableGeneralOptionsHandler: true, TLSConfig: nil,
		ReadTimeout: 0, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 0, IdleTimeout: 0, MaxHeaderBytes: 0, MaxHeaderValueCount: 0,
		TLSNextProto: nil, ConnState: nil, ErrorLog: nil, BaseContext: func(net.Listener) context.Context { return lifetime }, ConnContext: nil,
		HTTP2: nil, Protocols: nil, DisableClientPriority: false,
	}
	stop := context.AfterFunc(lifetime, func() {
		outcome.ForEachOption(outcome.EmptyableToOption(server.Close()), func(cause error) {
			fmt.Fprintln(os.Stderr, cause)
		})
	})
	defer stop()
	fmt.Fprintln(os.Stderr, "Listening on", listener.Addr())
	return server.Serve(listener)
}
