package native

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

type replyShapeWrong struct {
	Part    string
	Want    string
	Members []string
}

func (failed replyShapeWrong) Error() string {
	return fmt.Sprintf("%s requires %s, got fields %q", failed.Part, failed.Want, failed.Members)
}

func wrongReplyShape(part, want string, fields map[string]json.RawMessage) replyShapeWrong {
	return replyShapeWrong{Part: part, Want: want, Members: slices.Sorted(maps.Keys(fields))}
}

type replyFieldMissing struct {
	Part  string
	Field string
}

func (failed replyFieldMissing) Error() string {
	return fmt.Sprintf("%s is missing %s", failed.Part, failed.Field)
}

type replyFieldMistyped struct {
	Part  string
	Field string
	Want  string
	Read  json.RawMessage
}

func (failed replyFieldMistyped) Error() string {
	return fmt.Sprintf("%s %s must be %s, got %s", failed.Part, failed.Field, failed.Want, failed.Read)
}

type replyKindUnknown struct{ Name string }

func (failed replyKindUnknown) Error() string {
	return fmt.Sprintf("unknown reply variant %q", failed.Name)
}

type routeAdmitted struct{ Operation string }

func (failed routeAdmitted) Error() string {
	return fmt.Sprintf("route %s replied with a guard admission", failed.Operation)
}

type replyMissing struct{ Operation string }

func (failed replyMissing) Error() string {
	return fmt.Sprintf("%s returned no reply", failed.Operation)
}

type answerFieldsUnread struct{ Member string }

func (failed answerFieldsUnread) Error() string {
	return fmt.Sprintf("response fields for %s were not read", failed.Member)
}

type answerMemberUndeclared struct{ Tag string }

func (failed answerMemberUndeclared) Error() string {
	return fmt.Sprintf("response variant %q was not declared for this operation", failed.Tag)
}

type answerStatusMissing struct{ Tag string }

func (failed answerStatusMissing) Error() string {
	return fmt.Sprintf("response variant %q has no status", failed.Tag)
}

type parameterSchemaMissing struct{}

func (parameterSchemaMissing) Error() string { return "the request parameter schema is missing" }
