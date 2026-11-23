package running_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

type applicationWitness struct {
	Directory  string
	Arguments  []string
	ProcessID  int
	Interrupts int
}

func TestMain(tests *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "nocontract-application-probe" {
		os.Exit(probeApplication())
	}
	os.Exit(tests.Run())
}

func probeApplication() int {
	if len(os.Args) < 4 {
		return 2
	}
	failed := func(cause error) int { fmt.Fprintln(os.Stderr, cause); return 2 }
	return outcome.Fold(outcome.TupleToResult(os.Getwd()), func(directory string) int {
		witness := applicationWitness{Directory: directory, Arguments: os.Args[1:], ProcessID: os.Getpid(), Interrupts: 0}
		interrupts := make(chan os.Signal, 2)
		signal.Notify(interrupts, os.Interrupt)
		defer signal.Stop(interrupts)
		return outcome.Fold(outcome.TupleToResult(os.OpenFile(os.Args[2], os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)), func(output *os.File) int {
			defer func() {
				outcome.ForEachOption(outcome.EmptyableToOption(output.Close()), func(cause error) { fmt.Fprintln(os.Stderr, cause) })
			}()
			encoder := json.NewEncoder(output)
			if os.Args[3] == "interrupt" {
				return outcome.FoldOption(outcome.EmptyableToOption(json.NewEncoder(os.Stdout).Encode(witness)), failed, func() int {
					<-interrupts
					witness.Interrupts = 1
					for {
						select {
						case <-interrupts:
							witness.Interrupts++
						default:
							return outcome.FoldOption(outcome.EmptyableToOption(encoder.Encode(witness)), failed, func() int { return 0 })
						}
					}
				})
			}
			return outcome.FoldOption(outcome.EmptyableToOption(encoder.Encode(witness)), failed, func() int {
				if os.Args[3] == "fail" {
					return 17
				}
				return 0
			})
		}, failed)
	}, failed)
}

func TestServeChecksOutputsThenRunsTheApplicationInItsProject(t *testing.T) {
	directory := scratchproject.Ledger(t, filepath.Join("..", ".."))
	generated := runOn(t, "generate", directory)
	if _, written := generated.(running.FilesWritten); !written {
		t.Fatalf("generating the project: %v", generated.Lines())
	}
	file := filepath.Join(directory, "nocontract.json")
	executable := scratchproject.PathOrStop(t, "finding the test application", outcome.TupleToResult(os.Executable()))
	launch := func(command []string) running.Outcome {
		return running.Run(running.Request{Arguments: append([]string{"serve", "-project", file, "--"}, command...), Context: t.Context()})
	}
	for _, mode := range []string{"success", "fail"} {
		t.Run(mode, func(t *testing.T) {
			witnessFile := filepath.Join(t.TempDir(), "application.json")
			arguments := []string{"nocontract-application-probe", witnessFile, mode, "argument with a space", "--app-flag"}
			result := launch(append([]string{executable}, arguments...))
			wantLaunch := running.ApplicationLaunch{ProjectFile: file, Directory: directory, Command: running.ApplicationCommand{Name: executable, Arguments: arguments}, Path: executable}
			switch mode {
			case "success":
				finished, success := result.(running.ApplicationFinished)
				if !success || finished.Application.Launch.String() != wantLaunch.String() || finished.Application.ProcessID <= 0 {
					t.Fatalf("serve answered %#v; want the successful application and its launch", result)
				}
			case "fail":
				failed, refused := result.(running.ApplicationNotFinished)
				if !refused || failed.Application.Launch.String() != wantLaunch.String() || failed.Application.ProcessID <= 0 {
					t.Fatalf("serve answered %#v; want the failed application and its launch", result)
				}
				outcome.FoldOption(outcome.TupleToOption(errors.AsType[*exec.ExitError](failed.Cause)), func(cause *exec.ExitError) struct{} {
					if cause.ExitCode() != 17 {
						t.Errorf("exit status %d; want 17", cause.ExitCode())
					}
					return struct{}{}
				}, func() struct{} {
					t.Fatalf("terminal cause %T %v; want the original ExitError", failed.Cause, failed.Cause)
					return struct{}{}
				})
			}
			read := applicationWitness{Directory: "", Arguments: []string{}, ProcessID: 0, Interrupts: 0}
			contents := scratchproject.BytesOrStop(t, "reading the application's witness", outcome.TupleToResult(os.ReadFile(witnessFile)))
			scratchproject.StopOn(t, "decoding the application's witness", json.Unmarshal(contents, &read))
			if read.Directory != directory || !slices.Equal(read.Arguments, arguments) {
				t.Fatalf("application read %+v; want directory %s and arguments %q", read, directory, arguments)
			}
		})
	}
	t.Run("the executable cannot start", func(t *testing.T) {
		missing := filepath.Join(directory, "missing-application")
		result := launch([]string{missing, "kept argument"})
		failed, refused := result.(running.ApplicationNotStarted)
		if !refused || failed.Launch.Directory != directory || failed.Launch.ProjectFile != file || failed.Launch.Path != missing || !slices.Equal(failed.Launch.Command.Arguments, []string{"kept argument"}) || !errors.Is(failed.Cause, fs.ErrNotExist) {
			t.Fatalf("serve answered %#v; want the attempted command, directory, project and original OS cause", result)
		}
	})
	t.Run("the command is missing", func(t *testing.T) {
		result := launch([]string{})
		refused, usage := result.(running.UsageRefused)
		if !usage {
			t.Fatalf("serve answered %#v; want typed command usage", result)
		}
		if _, missing := refused.Problem.(running.MissingApplicationCommand); !missing || refused.ErrorCode().String() != "NC-1036" {
			t.Fatalf("serve answered %#v; want MissingApplicationCommand under NC-1036", result)
		}
	})
	t.Run("an interrupt reaches its separate process group once", func(t *testing.T) {
		assertApplicationInterrupt(t, directory, executable)
	})
	t.Run("stale output prevents the application from starting", func(t *testing.T) {
		witness := filepath.Join(t.TempDir(), "must-not-be-written.json")
		scratchproject.WriteFile(t, directory, "server/nocontract_server.go", []byte(description.GeneratedHeader+"package server\n"))
		result := launch([]string{executable, "nocontract-application-probe", witness, "success"})
		if _, stale := result.(running.FilesStale); !stale {
			t.Fatalf("serve answered %#v; want stale output before any application starts", result)
		}
		outcome.Fold(outcome.TupleToResult(os.Stat(witness)), func(fs.FileInfo) struct{} { t.Fatal("application started with stale outputs"); return struct{}{} }, func(cause error) struct{} {
			if !errors.Is(cause, fs.ErrNotExist) {
				t.Fatal(cause)
			}
			return struct{}{}
		})
	})
}
