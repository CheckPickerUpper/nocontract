package filecheck

import "base:runtime"
import "core:io"
import "core:os"
import "core:slice"
import "core:time"

// Check_Command is the project's Go tool invocation from its test directory.
Check_Command :: struct #all_or_none {
	project:   string,
	directory: string,
	arguments: []string,
}

// Check_Execution keeps the native process state and both captured byte streams.
Check_Execution :: struct #all_or_none {
	command: Check_Command,
	process: os.Process_State,
	stdout:  []u8,
	stderr:  []u8,
	elapsed: time.Duration,
}

// Current_Files is an invocation that exited successfully.
Current_Files :: struct #all_or_none {
	execution: Check_Execution,
}

// Arguments_Not_Copied is an invocation whose argument storage could not be allocated.
Arguments_Not_Copied :: struct #all_or_none {
	project: string,
	cause:   runtime.Allocator_Error,
	elapsed: time.Duration,
}

// Directory_Unreadable is a test directory the operating system could not resolve.
Directory_Unreadable :: struct #all_or_none {
	project:   string,
	arguments: []string,
	cause:     os.Error,
	elapsed:   time.Duration,
}

// Environment_Unreadable is an invocation whose inherited environment could not be read.
Environment_Unreadable :: struct #all_or_none {
	command: Check_Command,
	cause:   os.Error,
	elapsed: time.Duration,
}

// Command_Not_Completed keeps any process and output returned with a native execution error.
Command_Not_Completed :: struct #all_or_none {
	execution: Check_Execution,
	cause:     os.Error,
}

// Command_Exited keeps NoContract's unsuccessful exit and its original diagnostics.
Command_Exited :: struct #all_or_none {
	execution: Check_Execution,
}

// Check is every way the project's generated-file check can end.
Check :: union #no_nil {
	Current_Files,
	Arguments_Not_Copied,
	Directory_Unreadable,
	Environment_Unreadable,
	Command_Not_Completed,
	Command_Exited,
}

// check runs the declared Go tool dependency without a shell. Returned allocations belong to allocator.
@(require_results)
check :: proc(project: string, allocator: runtime.Allocator) -> Check {
	started := time.tick_now()
	arguments, copy_error := slice.clone([]string{"go", "tool", "nocontract", "check", "-project", project}, allocator)
	switch copy_error {
	case .None:
	case .Out_Of_Memory, .Invalid_Pointer, .Invalid_Argument, .Mode_Not_Implemented:
		return Arguments_Not_Copied{project = project, cause = copy_error, elapsed = time.tick_since(started)}
	}
	directory, directory_error := os.get_working_directory(allocator)
	switch _ in directory_error {
	case nil:
	case os.General_Error, io.Error, runtime.Allocator_Error, os.Platform_Error:
		return Directory_Unreadable{project = project, arguments = arguments, cause = directory_error, elapsed = time.tick_since(started)}
	}
	command := Check_Command{project = project, directory = directory, arguments = arguments}
	environment, environment_error := os.environ(allocator)
	switch _ in environment_error {
	case nil:
	case os.General_Error, io.Error, runtime.Allocator_Error, os.Platform_Error:
		return Environment_Unreadable{command = command, cause = environment_error, elapsed = time.tick_since(started)}
	}
	process, stdout, stderr, execution_error := os.process_exec(os.Process_Desc{working_dir = directory, command = arguments, env = environment}, allocator)
	for entry in environment {
		delete(entry, allocator)
	}
	delete(environment, allocator)
	execution := Check_Execution{command = command, process = process, stdout = stdout, stderr = stderr, elapsed = time.tick_since(started)}
	switch _ in execution_error {
	case nil:
		if process.exit_code == 0 {
			return Current_Files{execution = execution}
		}
		return Command_Exited{execution = execution}
	case os.General_Error, io.Error, runtime.Allocator_Error, os.Platform_Error:
		return Command_Not_Completed{execution = execution, cause = execution_error}
	}
	unreachable()
}
