package native

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"

	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/outcome"
)

type invocation struct {
	Operation string          `json:"operation"`
	Input     invocationInput `json:"input"`
}

//sumtype:decl
type invocationInput interface{ isInvocationInput() }

func (bodyObject) isInvocationInput() {}
func (guardInput) isInvocationInput() {}

//sumtype:decl
type reply interface{ isReply() }

//ergo:not-an-error
type answered struct {
	Tag    string          `json:"tag"`
	Fields json.RawMessage `json:"fields"`
}

//ergo:not-an-error
type admitted struct{}

func (answered) isReply() {}
func (admitted) isReply() {}

// MalformedReply records the operation, the reply it produced, and the shape it should have.
type MalformedReply struct {
	Operation string
	Line      string
	Cause     error
}

// Error identifies the operation and why its reply was refused.
func (failed MalformedReply) Error() string {
	return fmt.Sprintf("native operation %s returned an invalid reply: %v", failed.Operation, failed.Cause)
}

// RefusedInvocation records a native dispatcher that could not call the declared operation.
type RefusedInvocation struct {
	Operation string
	Reason    string
}

// Error identifies the operation and the dispatcher's refusal.
func (failed RefusedInvocation) Error() string {
	return fmt.Sprintf("native operation %s was refused: %s", failed.Operation, failed.Reason)
}

func decodeReply(operation string, line []byte) outcome.Result[reply] {
	bad := func(cause error) outcome.Result[reply] {
		return outcome.Err[reply](MalformedReply{Operation: operation, Line: string(line), Cause: cause})
	}
	written := map[string]json.RawMessage{}
	return outcome.Fold(outcome.TupleToResult(written, jsonv2.Unmarshal(line, &written)), func(fields map[string]json.RawMessage) outcome.Result[reply] {
		if len(fields) == 1 {
			for name, payload := range fields {
				return decodeReplyMember(operation, name, payload, bad)
			}
		}
		return bad(wrongReplyShape("reply", "one of answered, admitted, or refused", fields))
	}, bad)
}

func decodeReplyMember(operation, name string, payload json.RawMessage, bad func(error) outcome.Result[reply]) outcome.Result[reply] {
	fields := map[string]json.RawMessage{}
	return outcome.Fold(outcome.TupleToResult(fields, jsonv2.Unmarshal(payload, &fields)), func(fields map[string]json.RawMessage) outcome.Result[reply] {
		payload := bytes.TrimSpace(payload)
		if len(payload) == 0 || payload[0] != '{' {
			return bad(replyFieldMistyped{Part: "reply", Field: name, Want: "an object", Read: payload})
		}
		switch name {
		case "answered":
			if len(fields) != 2 {
				return bad(wrongReplyShape(name, "tag and fields", fields))
			}
			return outcome.FoldOption(keyed.Entry(fields, "fields"), func(body json.RawMessage) outcome.Result[reply] {
				return replyText(fields, name, "tag", func(tag string) outcome.Result[reply] {
					return outcome.Ok[reply](answered{Tag: tag, Fields: body})
				}, bad)
			}, func() outcome.Result[reply] { return bad(replyFieldMissing{Part: name, Field: "fields"}) })
		case "admitted":
			if len(fields) == 0 {
				return outcome.Ok[reply](admitted{})
			}
			return bad(wrongReplyShape(name, "an empty object", fields))
		case "refused":
			if len(fields) != 1 {
				return bad(wrongReplyShape(name, "reason", fields))
			}
			return replyText(fields, name, "reason", func(reason string) outcome.Result[reply] {
				return outcome.Err[reply](RefusedInvocation{Operation: operation, Reason: reason})
			}, bad)
		}
		return bad(replyKindUnknown{Name: name})
	}, bad)
}

func replyText(fields map[string]json.RawMessage, part, name string, next func(string) outcome.Result[reply],
	bad func(error) outcome.Result[reply],
) outcome.Result[reply] {
	return outcome.FoldOption(keyed.Entry(fields, name), func(raw json.RawMessage) outcome.Result[reply] {
		text := ""
		return outcome.Fold(outcome.TupleToResult(&text, jsonv2.Unmarshal(raw, &text)), func(text *string) outcome.Result[reply] {
			if *text == "" {
				return bad(replyFieldMistyped{Part: part, Field: name, Want: "nonempty text", Read: raw})
			}
			return next(*text)
		}, bad)
	}, func() outcome.Result[reply] { return bad(replyFieldMissing{Part: part, Field: name}) })
}
