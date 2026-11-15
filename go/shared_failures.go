package nocontract

import (
	"encoding/json"
	"net/http"

	"github.com/samber/mo"
)

// RequestPart is the part of a request a route's input reads.
type RequestPart string

// The parts of a request a route's input reads.
const (
	PartPath  RequestPart = "path"
	PartQuery RequestPart = "query"
	PartBody  RequestPart = "body"
)

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

// The statuses and codes nocontract answers its own failures with. Their directives above are what
// the generator writes into the TypeScript; TestSharedFailuresAnswerWhatTheirDirectivesDeclare
// holds the two together.
const (
	requestRefusedStatus   = http.StatusBadRequest
	requestRefusedCode     = "NC-1001"
	answerNotWrittenStatus = http.StatusInternalServerError
	answerNotWrittenCode   = "NC-1002"
)

// answer is the refusal as the answer the route writes.
func (refused RequestRefused) answer() Answer {
	return Failed(Failure{Status: requestRefusedStatus, Tag: "RequestRefused", Code: requestRefusedCode, Fields: mo.TupleToResult(json.Marshal(refused))})
}

// answer is the failure as the answer the route writes.
func (failed AnswerNotWritten) answer() Answer {
	return Failed(Failure{Status: answerNotWrittenStatus, Tag: "AnswerNotWritten", Code: answerNotWrittenCode, Fields: mo.TupleToResult(json.Marshal(failed))})
}
