// Package native connects compiler-checked Odin and Gleam handlers to NoContract's HTTP runtime.
package native

import (
	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
)

// Request names the project folder, packages to read, API, and generated server folder.
type Request struct {
	Directory string
	Packages  []string
	API       assembling.NamedAPI
	Server    string
}

// Project is a compiler's declarations and the native bindings for serving them.
type Project struct {
	Declared assembling.Declared
	Guards   []GuardedRoute
	Generate func() outcome.Result[[]File]
	Command  Command
}

// GuardedRoute is the guard a route calls with Scope and the refusals it can return.
type GuardedRoute struct {
	Group    string
	Endpoint string
	Scope    string
	Refusals []description.Member
}

// File is a generated native binding at its absolute path.
type File struct {
	Path     string
	Contents []byte
}

// Command starts the generated dispatcher in the project that owns it.
// Arguments holds the arguments to pass to Executable.
type Command struct {
	Directory  string
	Executable string
	Arguments  []string
}

// GuardOperationID identifies the native guard invocation for a route.
func GuardOperationID(group, endpoint string) string {
	return description.OperationID(group, endpoint) + "#guard"
}
