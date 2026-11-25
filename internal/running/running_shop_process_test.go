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

// shopOutputPipes are the shop's standard output and standard error, both read by the test.
type shopOutputPipes struct {
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func openShopOutput(command *exec.Cmd) outcome.Result[shopOutputPipes] {
	return outcome.Fold(outcome.TupleToResult(command.StdoutPipe()), func(stdout io.ReadCloser) outcome.Result[shopOutputPipes] {
		return outcome.Fold(outcome.TupleToResult(command.StderrPipe()), func(stderr io.ReadCloser) outcome.Result[shopOutputPipes] {
			return outcome.Ok(shopOutputPipes{stdout: stdout, stderr: stderr})
		}, outcome.Err[shopOutputPipes])
	}, outcome.Err[shopOutputPipes])
}

func openShopProcess(t *testing.T, command *exec.Cmd, shutdown shopProcessShutdown) shopServerAddress {
	t.Helper()
	diagnostics, emitted := &bytes.Buffer{}, &bytes.Buffer{}
	return outcome.Fold(openShopOutput(command), func(output shopOutputPipes) shopServerAddress {
		scratchproject.StopOn(t, "starting the "+command.Path+" shop", command.Start())
		readers := &errgroup.Group{}
		// Closed once the shop's standard error ended, after which diagnostics holds all of it.
		stderrEnded := make(chan struct{})
		readers.Go(func() error {
			defer close(stderrEnded)
			_, cause := io.Copy(diagnostics, output.stderr)
			return cause
		})
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
		lines := bufio.NewReader(output.stdout)
		notices := newShopListeningNotices()
		for {
			line := scratchproject.PathOrStop(t, "reading the shop's listening address", outcome.TupleToResult(lines.ReadString('\n')))
			address, listening := strings.CutPrefix(strings.TrimSpace(line), "Listening on ")
			if !listening {
				continue
			}
			readers.Go(func() error {
				scanner := bufio.NewScanner(lines)
				scanner.Buffer(make([]byte, 4096), 1<<20)
				for scanner.Scan() {
					line := scanner.Text()
					emitted.WriteString(line)
					emitted.WriteByte('\n')
					if address, listening := strings.CutPrefix(strings.TrimSpace(line), "Listening on "); listening {
						notices.add(shopListening{address: address})
					}
				}
				<-stderrEnded
				notices.add(shopExited{stderr: lastLines(diagnostics.String(), shopExitStderrLines)})
				return scanner.Err()
			})
			client := *http.DefaultClient
			client.Timeout = 10 * time.Second
			return shopServerAddress{url: address, client: &client, finish: finish, notices: notices, pid: command.Process.Pid}
		}
	}, func(cause error) shopServerAddress {
		t.Fatalf("opening the shop's output: %v", cause)
		return shopServerAddress{url: "", client: http.DefaultClient, finish: func() []string { return []string{} }, notices: newShopListeningNotices(), pid: 0}
	})
}
