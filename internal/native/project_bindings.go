// Package native describes compiler-checked projects and their execution plans.
package native

import (
	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
)

// Request names the project folder, packages to read, API, and generated server folder.
type Request struct {
	Directory       string
	Packages        []string
	API             assembling.NamedAPI
	Server          string
	OdinCollections []OdinCollection
}

// OdinCollection is a named directory that Odin searches for imported packages.
type OdinCollection struct {
	Name      string
	Directory string
}

// Project is a compiler's declarations and the native bindings for serving them.
type Project struct {
	Declared  assembling.Declared
	Generate  func(assembling.APIAssembled) outcome.Result[[]File]
	Execution Execution
}

// Execution is the compiler backend's serving plan.
//
//sumtype:decl
type Execution interface{ isExecution() }

// OdinDispatcher invokes typed Odin procedures through its generated stdin/stdout dispatcher.
//
//ergo:not-an-error
type OdinDispatcher struct {
	Command Command
	Guards  []GuardedRoute
}

// GleamListener is started by the application's generated Wisp/Mist module on its BEAM.
//
//ergo:not-an-error
type GleamListener struct{ Directory string }

func (OdinDispatcher) isExecution() {}
func (GleamListener) isExecution()  {}

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
