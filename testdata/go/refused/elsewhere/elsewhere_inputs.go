// Package elsewhere declares route inputs that routes in another package read, each with a body
// the generated code there cannot write.
package elsewhere

// PlacedInput's body holds an unexported field, which makes its struct another type wherever else
// it is written.
type PlacedInput struct {
	Body struct {
		Amount int `json:"amount"`
		note   string
	}
}

// hiddenBody is a body type this package does not export.
type hiddenBody struct {
	Amount int `json:"amount"`
}

// HiddenInput's body is a type no other package can name.
type HiddenInput struct {
	Body hiddenBody
}
