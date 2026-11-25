package supervisor

import "base:runtime"
import "core:c"
import hex "core:encoding/hex"
import "core:fmt"
import "core:io"
import "core:mem"
import "core:os"
import "core:slice"
import "core:strconv"
import "core:sys/linux"
import "core:sys/posix"

// Supervisor_Entry holds the current program's original arguments and inherited environment.
@(not_an_error)
Supervisor_Entry :: struct #all_or_none {
	arguments:   []string,
	environment: []string,
}

// Worker_Entry is the private control received by a re-executed worker: its readiness pipe,
// the listening socket it inherited from the supervisor, and the supervisor's process.
@(not_an_error)
Worker_Entry :: struct #all_or_none {
	readiness:     posix.FD,
	listener:      posix.FD,
	parent:        posix.pid_t,
	original_mask: posix.sigset_t,
}

// Program_Entry distinguishes the supervisor from its worker or rejected control values.
Program_Entry :: union #no_nil {
	Supervisor_Entry,
	Worker_Entry,
	Entry_Refusal,
}

@(private)
Control_Names :: struct #all_or_none {
	readiness: cstring,
	listener:  cstring,
	parent:    cstring,
	mask:      cstring,
}

@(private, require_results)
control_names :: proc() -> Control_Names {
	return Control_Names{
		readiness = "NOCONTRACT_WORKER_READINESS",
		listener = "NOCONTRACT_WORKER_LISTENER",
		parent = "NOCONTRACT_WORKER_PARENT",
		mask = "NOCONTRACT_WORKER_MASK",
	}
}

// Control_Value retains a private environment field's name and exact text.
Control_Value :: struct #all_or_none {
	name:  string,
	value: string,
}

// No_Control_Value identifies the exact private environment field that was absent.
@(not_an_error)
No_Control_Value :: struct #all_or_none {
	name: string,
}

// Control_Read retains a present value or names the absent environment field.
Control_Read :: union #no_nil {
	No_Control_Value,
	Control_Value,
}

// Control_Values retains every private value read when the control record is incomplete.
Control_Values :: struct #all_or_none {
	readiness: Control_Read,
	listener:  Control_Read,
	parent:    Control_Read,
	mask:      Control_Read,
}

// enter reads and clears private controls before returning the current program's role.
// Allocations belong to the caller's allocator and remain valid with returned evidence.
@(require_results)
enter :: proc(allocator: mem.Allocator) -> Program_Entry {
	names := control_names()
	values := Control_Values{
		readiness = read_control(string(names.readiness), allocator),
		listener = read_control(string(names.listener), allocator),
		parent = read_control(string(names.parent), allocator),
		mask = read_control(string(names.mask), allocator),
	}
	names_to_clear := []cstring{names.readiness, names.listener, names.parent, names.mask}
	for name in names_to_clear {
		switch posix.unsetenv(name) {
		case .OK:
		case .FAIL:
			return Entry_Refusal(Control_Not_Cleared{name = string(name), values = values, cause = posix.errno()})
		}
	}
	switch readiness in values.readiness {
	case No_Control_Value:
		switch _ in values.listener {
		case No_Control_Value:
		case Control_Value:
			return Entry_Refusal(Control_Incomplete{values = values})
		}
		switch _ in values.parent {
		case No_Control_Value:
		case Control_Value:
			return Entry_Refusal(Control_Incomplete{values = values})
		}
		switch _ in values.mask {
		case No_Control_Value:
		case Control_Value:
			return Entry_Refusal(Control_Incomplete{values = values})
		}
		environment, environment_error := os.environ(allocator)
		switch _ in environment_error {
		case nil:
			return Supervisor_Entry{arguments = os.args, environment = environment}
		case os.General_Error, io.Error, runtime.Allocator_Error, os.Platform_Error:
			return Entry_Refusal(Environment_Not_Read{cause = environment_error})
		}
	case Control_Value:
		return read_worker(readiness, values)
	}
	unreachable()
}

@(private, require_results)
read_control :: proc(name: string, allocator: mem.Allocator) -> Control_Read {
	value, found := os.lookup_env(name, allocator)
	if found {
		return Control_Value{name = name, value = value}
	}
	return No_Control_Value{name = name}
}

@(private)
Control_Number :: struct #all_or_none {
	value: int,
}

@(private)
Control_Number_Read :: union #no_nil {
	Control_Number,
	Entry_Refusal,
}

@(private, require_results)
read_control_number :: proc(field: Control_Value, least, most: int) -> Control_Number_Read {
	value, parsed := strconv.parse_int(field.value, 10)
	if !parsed {
		return Entry_Refusal(Control_Not_Integer{field = field})
	}
	if value < least || value > most {
		return Entry_Refusal(Control_Integer_Outside{field = field, read = value, least = least, most = most})
	}
	return Control_Number{value = value}
}

@(private, require_results)
read_worker :: proc(readiness: Control_Value, values: Control_Values) -> Program_Entry {
	switch descriptor in read_control_number(readiness, 3, int(max(c.int))) {
	case Entry_Refusal:
		return descriptor
	case Control_Number:
		switch listener in values.listener {
		case No_Control_Value:
			return Entry_Refusal(Control_Incomplete{values = values})
		case Control_Value:
			switch inherited in read_control_number(listener, 3, int(max(c.int))) {
			case Entry_Refusal:
				return inherited
			case Control_Number:
				return read_worker_parent(posix.FD(descriptor.value), posix.FD(inherited.value), values)
			}
		}
	}
	unreachable()
}

@(private, require_results)
read_worker_parent :: proc(descriptor: posix.FD, listener: posix.FD, values: Control_Values) -> Program_Entry {
	switch parent in values.parent {
	case No_Control_Value:
		return Entry_Refusal(Control_Incomplete{values = values})
	case Control_Value:
		switch parsed in read_control_number(parent, 1, int(max(posix.pid_t))) {
		case Entry_Refusal:
			return parsed
		case Control_Number:
			switch field in values.mask {
			case No_Control_Value:
				return Entry_Refusal(Control_Incomplete{values = values})
			case Control_Value:
				if len(field.value) != 2 * size_of(posix.sigset_t) {
					return Entry_Refusal(Control_Mask_Width_Differed{field = field, read = len(field.value), expected = 2 * size_of(posix.sigset_t)})
				}
				mask := posix.sigset_t{}
				_, accepted := hex.decode_into_buffer(transmute([]u8)field.value, (cast(^[size_of(posix.sigset_t)]u8)&mask)[:])
				if !accepted {
					return Entry_Refusal(Control_Mask_Not_Hex{field = field})
				}
				return bind_to_supervisor(Worker_Entry{readiness = descriptor, listener = listener, parent = posix.pid_t(parsed.value), original_mask = mask})
			}
		}
	}
	unreachable()
}

// bind_to_supervisor makes the kernel kill the worker when its supervisor ends, then checks the
// supervisor did not end before that request, so no worker keeps serving the inherited socket alone.
// The kernel sends the signal when the supervisor's thread that started the worker ends; that is
// the thread blocked in supervise.
@(private, require_results)
bind_to_supervisor :: proc(worker: Worker_Entry) -> Program_Entry {
	PR_SET_PDEATHSIG :: 1
	// core:sys/linux prctl reads four arguments whatever the caller passes.
	cause := linux.prctl(PR_SET_PDEATHSIG, uint(linux.Signal.SIGKILL), 0, 0, 0)
	if cause != .NONE {
		return Entry_Refusal(Parent_Death_Signal_Not_Set{cause = cause})
	}
	parent := linux.getppid()
	if posix.pid_t(parent) != worker.parent {
		return Entry_Refusal(Parent_Not_Supervisor{supervisor = worker.parent, parent = parent})
	}
	return worker
}

@(private, not_an_error)
Environment_Assembled :: struct #all_or_none {
	values: []string,
	controls: [4]string,
	encoded_mask: []u8,
}

@(private)
Environment_Assembly :: union #no_nil {
	Environment_Assembled,
	Supervision_Refusal,
}

@(private, require_results)
worker_environment :: proc(environment: []string, worker: Worker_Entry, allocator: mem.Allocator) -> Environment_Assembly {
	names := control_names()
	mask := worker.original_mask
	encoded_mask, encoding_error := hex.encode((cast(^[size_of(posix.sigset_t)]u8)&mask)[:], allocator)
	if encoding_error != .None {
		return Supervision_Refusal(Mask_Not_Encoded{mask = mask, cause = encoding_error})
	}
	controls := [4]string{
		fmt.aprintf("%s=%v", names.readiness, worker.readiness, allocator = allocator),
		fmt.aprintf("%s=%v", names.listener, worker.listener, allocator = allocator),
		fmt.aprintf("%s=%v", names.parent, worker.parent, allocator = allocator),
		fmt.aprintf("%s=%s", names.mask, encoded_mask, allocator = allocator),
	}
	values, assembly_error := slice.concatenate([][]string{controls[:], environment}, allocator)
	if assembly_error != .None {
		for control in controls {
			delete(control, allocator)
		}
		delete(encoded_mask, allocator)
		return Supervision_Refusal(Environment_Not_Assembled{worker = worker, cause = assembly_error})
	}
	return Environment_Assembled{values = values, controls = controls, encoded_mask = encoded_mask}
}

@(private)
free_environment :: proc(environment: Environment_Assembled, allocator: mem.Allocator) {
	delete(environment.values, allocator)
	for control in environment.controls {
		delete(control, allocator)
	}
	delete(environment.encoded_mask, allocator)
}
