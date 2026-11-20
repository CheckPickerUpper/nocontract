package native

import "fmt"

// DispatcherStartFailed preserves the launch command, startup step, and compiler diagnostics.
type DispatcherStartFailed struct {
	Command     Command
	Step        string
	Diagnostics string
	Cause       error
}

// Error describes the startup failure and any diagnostics the process wrote.
func (failed DispatcherStartFailed) Error() string {
	message := fmt.Sprintf("%s: %s: %v", describeCommand(failed.Command), failed.Step, failed.Cause)
	if failed.Diagnostics != "" {
		return message + "\n" + failed.Diagnostics
	}
	return message
}

// Unwrap returns the startup step's cause.
func (failed DispatcherStartFailed) Unwrap() error { return failed.Cause }

// InvocationFailed identifies an operation the native dispatcher could not complete.
type InvocationFailed struct {
	Command   Command
	Step      string
	Operation string
	Cause     error
}

// Error identifies the operation and the failed dispatch step.
func (failed InvocationFailed) Error() string {
	return fmt.Sprintf("%s: %s for %s: %v", describeCommand(failed.Command), failed.Step, failed.Operation, failed.Cause)
}

// Unwrap returns the dispatch step's cause.
func (failed InvocationFailed) Unwrap() error { return failed.Cause }

func describeCommand(command Command) string {
	return fmt.Sprintf("%q in %q", append([]string{command.Executable}, command.Arguments...), command.Directory)
}

type dispatcherNotReady struct{ Line string }

func (failed dispatcherNotReady) Error() string {
	return fmt.Sprintf("expected native dispatcher readiness, got %q", failed.Line)
}
