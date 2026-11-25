package supervisor

import "base:runtime"
import "core:c"
import "core:os"
import "core:sys/linux"
import "core:sys/posix"
import "core:time"

// Shutdown_Period_Not_Positive retains a period refused before a worker starts.
Shutdown_Period_Not_Positive :: struct #all_or_none {
	period: time.Duration,
}

// Worker_Shutdown_Expired retains the worker that outlived the shutdown period and the first
// shutdown request it was sent.
Worker_Shutdown_Expired :: struct #all_or_none {
	process: os.Process,
	signal:  posix.Signal,
	period:  time.Duration,
}

// Control_Not_Cleared retains the control record and the environment deletion failure.
Control_Not_Cleared :: struct #all_or_none {
	name:   string,
	values: Control_Values,
	cause:  posix.Errno,
}

// Control_Incomplete retains every present and absent control field.
Control_Incomplete :: struct #all_or_none {
	values: Control_Values,
}

// Control_Not_Integer retains an environment field the integer parser refused.
Control_Not_Integer :: struct #all_or_none {
	field: Control_Value,
}

// Control_Integer_Outside retains the original text, parsed value and accepted range.
Control_Integer_Outside :: struct #all_or_none {
	field: Control_Value,
	read:  int,
	least: int,
	most:  int,
}

// Control_Mask_Not_Hex is a private signal mask whose text is not hexadecimal.
Control_Mask_Not_Hex :: struct #all_or_none {
	field: Control_Value,
}

// Control_Mask_Width_Differed retains the decoded bytes and required native width.
Control_Mask_Width_Differed :: struct #all_or_none {
	field:    Control_Value,
	read:     int,
	expected: int,
}

// Environment_Not_Read keeps the core library's original environment failure.
Environment_Not_Read :: struct #all_or_none {
	cause: os.Error,
}

// Mask_Not_Encoded retains the native mask and the encoding allocation failure.
Mask_Not_Encoded :: struct #all_or_none {
	mask:  posix.sigset_t,
	cause: runtime.Allocator_Error,
}

// Environment_Not_Assembled retains the requested control and allocation failure.
Environment_Not_Assembled :: struct #all_or_none {
	worker: Worker_Entry,
	cause:  runtime.Allocator_Error,
}

// Pipe_Not_Opened preserves the readiness pipe creation failure.
Pipe_Not_Opened :: struct #all_or_none {
	cause: posix.Errno,
}

// Pipe_Not_Configured preserves the exact descriptor, fcntl command and argument.
Pipe_Not_Configured :: struct #all_or_none {
	descriptor: posix.FD,
	command:    posix.FCNTL_Cmd,
	argument:   c.int,
	cause:      posix.Errno,
}

// Pipe_Not_Closed preserves a failed descriptor release.
Pipe_Not_Closed :: struct #all_or_none {
	descriptor: posix.FD,
	cause:      posix.Errno,
}

// Readiness_Not_Written keeps the intended port, write count and original system error.
Readiness_Not_Written :: struct #all_or_none {
	worker:   Worker_Entry,
	port:     u16,
	expected: int,
	cause:    posix.Errno,
}

// Readiness_Write_Short keeps a partial atomic write separately from a failed system call.
Readiness_Write_Short :: struct #all_or_none {
	worker: Worker_Entry,
	port: u16,
	expected: int,
	written: int,
}

// Ready_Port_Zero preserves a worker attempting to announce an unassigned port.
Ready_Port_Zero :: struct #all_or_none {
	worker: Worker_Entry,
}

// Readiness_Not_Read retains the worker, readiness descriptor and read failure.
Readiness_Not_Read :: struct #all_or_none {
	worker:     os.Process,
	descriptor: posix.FD,
	cause:      posix.Errno,
}

// Readiness_Width_Differed retains the bytes read and required message width.
Readiness_Width_Differed :: struct #all_or_none {
	worker:   os.Process,
	packet:   [size_of(u16)]u8,
	received: int,
	expected: int,
}

// Readiness_Port_Zero is a worker announcing an address the kernel did not assign.
Readiness_Port_Zero :: struct #all_or_none {
	worker: os.Process,
}

// Readiness_Port_Differed retains the established and newly reported ports.
Readiness_Port_Differed :: struct #all_or_none {
	worker:   os.Process,
	expected: u16,
	reported: u16,
}

// Signal_Mask_Not_Changed preserves the exact mask operation and native failure.
Signal_Mask_Not_Changed :: struct #all_or_none {
	operation: posix.Sig,
	mask:      posix.sigset_t,
	cause:     posix.Errno,
}

// Signal_Not_Received preserves the waited signal set and native wait failure.
Signal_Not_Received :: struct #all_or_none {
	mask:  posix.sigset_t,
	cause: posix.Errno,
}

// Signal_Not_Sent preserves the recipient, signal and native kill failure.
Signal_Not_Sent :: struct #all_or_none {
	process: posix.pid_t,
	signal:  posix.Signal,
	cause:   posix.Errno,
}

// Child_Not_Observed preserves the exact worker and waitid failure.
Child_Not_Observed :: struct #all_or_none {
	process: os.Process,
	cause:   posix.Errno,
}

// Child_Report_Differed preserves a waitid report that does not describe the worker's death.
Child_Report_Differed :: struct #all_or_none {
	process: os.Process,
	report:  posix.siginfo_t,
}

// Child_Not_Reaped retains the native death report and core process_wait failure.
Child_Not_Reaped :: struct #all_or_none {
	process: os.Process,
	report:  posix.siginfo_t,
	cause:   os.Error,
}

// Worker_Not_Started preserves the original command, worker controls and core start failure.
Worker_Not_Started :: struct #all_or_none {
	arguments: []string,
	worker:    Worker_Entry,
	cause:     os.Error,
}

// Cleanup_Child_Not_Reaped preserves a process_wait failure during failed-start cleanup.
Cleanup_Child_Not_Reaped :: struct #all_or_none {
	process: os.Process,
	cause: os.Error,
}

// Signal_Outside_Set preserves an unexpected native result rather than hiding it.
Signal_Outside_Set :: struct #all_or_none {
	signal: posix.Signal,
	mask: posix.sigset_t,
}

// Pending_Signals_Not_Read retains the native pending-set query failure.
Pending_Signals_Not_Read :: struct #all_or_none {
	cause: posix.Errno,
}

// Signal_Membership_Not_Read retains the queried set, signal and native failure.
Signal_Membership_Not_Read :: struct #all_or_none {
	signal: posix.Signal,
	mask: posix.sigset_t,
	cause: posix.Errno,
}

// Parent_Death_Signal_Not_Set keeps the prctl failure that would let a worker outlive its
// supervisor.
Parent_Death_Signal_Not_Set :: struct #all_or_none {
	cause: linux.Errno,
}

// Parent_Not_Supervisor is a worker whose parent is no longer the supervisor that started it,
// because the supervisor ended before the worker asked to die with it.
Parent_Not_Supervisor :: struct #all_or_none {
	supervisor: posix.pid_t,
	parent:     linux.Pid,
}

// Listener_Not_Inheritable keeps the descriptor whose close-on-exec flag could not be cleared.
Listener_Not_Inheritable :: struct #all_or_none {
	descriptor: posix.FD,
	command:    posix.FCNTL_Cmd,
	cause:      posix.Errno,
}

// Entry_Refusal keeps a private worker control record that could not be read or cleared, or a
// worker that could not bind its lifetime to its supervisor.
Entry_Refusal :: union #no_nil {
	Control_Incomplete,
	Control_Integer_Outside,
	Control_Mask_Not_Hex,
	Control_Mask_Width_Differed,
	Control_Not_Cleared,
	Control_Not_Integer,
	Environment_Not_Read,
	Parent_Death_Signal_Not_Set,
	Parent_Not_Supervisor,
}

// Readiness_Report_Refusal keeps the worker-side step that could not publish its bound port.
Readiness_Report_Refusal :: union #no_nil {
	Pipe_Not_Closed,
	Readiness_Not_Written,
	Readiness_Write_Short,
	Ready_Port_Zero,
	Signal_Mask_Not_Changed,
	Signal_Not_Sent,
}

// Supervision_Refusal keeps the supervisor-side native or protocol step that failed while it
// started, watched, replaced or released a worker.
Supervision_Refusal :: union #no_nil {
	Child_Not_Observed,
	Child_Not_Reaped,
	Child_Report_Differed,
	Cleanup_Child_Not_Reaped,
	Environment_Not_Assembled,
	Listener_Not_Inheritable,
	Mask_Not_Encoded,
	Pipe_Not_Closed,
	Pipe_Not_Configured,
	Pipe_Not_Opened,
	Pending_Signals_Not_Read,
	Readiness_Not_Read,
	Readiness_Port_Differed,
	Readiness_Port_Zero,
	Readiness_Width_Differed,
	Signal_Mask_Not_Changed,
	Signal_Membership_Not_Read,
	Signal_Not_Received,
	Signal_Not_Sent,
	Signal_Outside_Set,
	Worker_Not_Started,
}
