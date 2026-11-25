package running_test

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/sync/errgroup"
)

type shopProcessShutdown struct {
	stop   func()
	verify func(error)
}

type shopProcessOutput struct {
	lines []string
	read  outcome.Result[struct{}]
	wait  outcome.Result[struct{}]
}

func openShopProcess(t *testing.T, command *exec.Cmd, shutdown shopProcessShutdown) shopServerAddress {
	t.Helper()
	diagnostics, emitted := &bytes.Buffer{}, &bytes.Buffer{}
	command.Stderr = diagnostics
	return outcome.Fold(outcome.TupleToResult(command.StdoutPipe()), func(stdout io.ReadCloser) shopServerAddress {
		scratchproject.StopOn(t, "starting the "+command.Path+" shop", command.Start())
		readers := &errgroup.Group{}
		collected := sync.OnceValue(func() shopProcessOutput {
			shutdown.stop()
			read := outcome.TupleToResult(struct{}{}, readers.Wait())
			wait := outcome.TupleToResult(struct{}{}, command.Wait())
			return shopProcessOutput{lines: strings.Split(diagnostics.String()+"\n"+emitted.String(), "\n"), read: read, wait: wait}
		})
		verified := sync.OnceFunc(func() {
			output := collected()
			outcome.Fold(output.read, func(struct{}) struct{} { return struct{}{} }, func(cause error) struct{} { t.Errorf("reading the shop output: %v", cause); return struct{}{} })
			outcome.Fold(output.wait, func(struct{}) struct{} { return struct{}{} }, func(cause error) struct{} {
				t.Logf("Shop process output:\n%s", strings.Join(output.lines, "\n"))
				shutdown.verify(cause)
				return struct{}{}
			})
		})
		finish := func() []string { output := collected(); verified(); return output.lines }
		t.Cleanup(func() { finish() })
		lines := bufio.NewReader(stdout)
		ready := make(chan string, 1)
		for {
			line := scratchproject.PathOrStop(t, "reading the shop's listening address", outcome.TupleToResult(lines.ReadString('\n')))
			address, listening := strings.CutPrefix(strings.TrimSpace(line), "Listening on ")
			if !listening {
				continue
			}
			readers.Go(func() error {
				defer close(ready)
				scanner := bufio.NewScanner(lines)
				scanner.Buffer(make([]byte, 4096), 1<<20)
				for scanner.Scan() {
					line := scanner.Text()
					emitted.WriteString(line)
					emitted.WriteByte('\n')
					if address, listening := strings.CutPrefix(strings.TrimSpace(line), "Listening on "); listening {
						select {
						case ready <- address:
						default:
						}
					}
				}
				return scanner.Err()
			})
			client := *http.DefaultClient
			client.Timeout = 10 * time.Second
			return shopServerAddress{url: address, client: &client, finish: finish, ready: ready, pid: command.Process.Pid}
		}
	}, func(cause error) shopServerAddress {
		t.Fatalf("opening shop stdout: %v", cause)
		ready := make(chan string)
		close(ready)
		return shopServerAddress{url: "", client: http.DefaultClient, finish: func() []string { return []string{} }, ready: ready, pid: 0}
	})
}
