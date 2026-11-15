package nocontract

// RequestPart is the part of a request a route's input reads.
type RequestPart string

// The parts of a request a route's input reads.
const (
	PartPath  RequestPart = "path"
	PartQuery RequestPart = "query"
	PartBody  RequestPart = "body"
)

//go:generate go run ../internal/cmd/runtime-answers generate

// SharedFailure is a failure nocontract answers on a route's behalf, outside its handler. Each
// member's status is the one its directive declares, which the generated
// NocontractAnswerSharedFailure answers with and the generator writes into the TypeScript.
//
//sumtype:decl
type SharedFailure interface{ isSharedFailure() }

func (RequestRefused) isSharedFailure()   {}
func (AnswerNotWritten) isSharedFailure() {}

// RequestRefused is a request whose path segment, query parameter or body the route's input could
// not read. Every route that reads its request can answer it, before its handler runs.
//
//ergo:code NC-1001
//nocontract:status 400
type RequestRefused struct {
	Part   RequestPart `json:"part"`
	Name   string      `json:"name"`
	Reason string      `json:"reason"`
}

// AnswerNotWritten is a route whose outcome could not be written as JSON, or that answered no
// outcome at all. Every route can answer it.
//
//ergo:code NC-1002
//nocontract:status 500
type AnswerNotWritten struct {
	Answer string `json:"answer"`
	Reason string `json:"reason"`
}
