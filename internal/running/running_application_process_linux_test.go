package running_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/internal/running"
	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
)

type applicationNoticePipe struct {
	read  *os.File
	write *os.File
}

func openApplicationNoticePipe() outcome.Result[applicationNoticePipe] {
	read, write, cause := os.Pipe()
	return outcome.TupleToResult(applicationNoticePipe{read: read, write: write}, cause)
}

func assertApplicationInterrupt(t *testing.T, directory, executable string) {
	t.Helper()
	witnessFile := filepath.Join(t.TempDir(), "interrupted-application.json")
	lifetime, cancel := context.WithCancel(t.Context())
	defer cancel()
	outcome.Fold(openApplicationNoticePipe(), func(pipe applicationNoticePipe) struct{} {
		defer func() {
			outcome.ForEachOption(outcome.EmptyableToOption(pipe.read.Close()), func(cause error) { t.Error(cause) })
		}()
		previousOutput := os.Stdout
		os.Stdout = pipe.write
		defer func() { os.Stdout = previousOutput }()
		finished := make(chan running.Outcome, 1)
		go func() {
			defer close(finished)
			finished <- running.Run(running.Request{Arguments: []string{"serve", "-project", filepath.Join(directory, "nocontract.json"), "--", executable, "nocontract-application-probe", witnessFile, "interrupt"}, Context: lifetime})
		}()
		defer func() {
			cancel()
			for range finished {
			}
		}()
		ready := make(chan outcome.Result[applicationWitness], 1)
		go func() {
			witness := applicationWitness{Directory: "", Arguments: []string{}, ProcessID: 0, Interrupts: 0}
			cause := json.NewDecoder(pipe.read).Decode(&witness)
			ready <- outcome.TupleToResult(witness, cause)
		}()
		select {
		case result := <-finished:
			scratchproject.StopOn(t, "closing the unused application output", pipe.write.Close())
			t.Fatalf("application stopped before its ready notice: %v", result.Lines())
		case notice := <-ready:
			os.Stdout = previousOutput
			scratchproject.StopOn(t, "closing the CLI's copy of application output", pipe.write.Close())
			outcome.Fold(notice, func(ready applicationWitness) struct{} {
				outcome.Fold(outcome.TupleToResult(syscall.Getpgid(ready.ProcessID)), func(group int) struct{} {
					if group != ready.ProcessID || group == syscall.Getpgrp() {
						t.Errorf("application %d has group %d, CLI group %d; want its own group", ready.ProcessID, group, syscall.Getpgrp())
					}
					return struct{}{}
				}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
				cancel()
				result := <-finished
				if terminal, stopped := result.(running.ApplicationStopped); !stopped || terminal.Application.ProcessID != ready.ProcessID {
					t.Errorf("serve answered %#v; want the interrupted application to stop successfully", result)
				}
				stopped := applicationWitness{Directory: "", Arguments: []string{}, ProcessID: 0, Interrupts: 0}
				contents := scratchproject.BytesOrStop(t, "reading the interrupted application", outcome.TupleToResult(os.ReadFile(witnessFile)))
				scratchproject.StopOn(t, "decoding the interrupted application", json.Unmarshal(contents, &stopped))
				if stopped.ProcessID != ready.ProcessID || stopped.Interrupts != 1 || stopped.Directory != directory {
					t.Errorf("interrupted application = %+v; ready = %+v; want exactly one interrupt", stopped, ready)
				}
				return struct{}{}
			}, func(cause error) struct{} {
				t.Fatalf("reading the application's ready notice: %v", cause)
				return struct{}{}
			})
		}
		return struct{}{}
	}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
}
