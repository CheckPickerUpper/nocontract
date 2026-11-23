// Package native describes compiler-checked declarations and their generated bindings.
package native

import (
	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/assembling"
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

// Project is a compiler's declarations and its bindings generated from the common checked API.
type Project struct {
	Declared assembling.Declared
	Generate func(assembling.APIAssembled) outcome.Result[[]File]
}

// File is a generated native binding at its absolute path.
type File struct {
	Path     string
	Contents []byte
}
