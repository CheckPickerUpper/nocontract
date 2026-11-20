package native

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

type memberAnswers map[string]func(json.RawMessage) nocontract.Answer

type routeGuard func(*http.Request, func() nocontract.Answer) nocontract.Answer

// Handler serves the same checked HTTP contract that NoContract uses to generate the clients.
func Handler(checked assembling.APIAssembled, guards []GuardedRoute, dispatcher Dispatcher, layers []nocontract.Layer) http.Handler {
	api := checked.API
	requests, answers := requestBodies(api.Structs), answerBodies(api.Structs)
	guarded := make(map[string]routeGuard, len(guards))
	for _, guard := range guards {
		operation := description.OperationID(guard.Group, guard.Endpoint)
		guarded[operation] = guardRequest(GuardOperationID(guard.Group, guard.Endpoint), guard, answers, dispatcher)
	}
	endpoints := []nocontract.Endpoint{}
	for _, group := range api.Groups {
		for _, route := range group.Endpoints {
			operation := description.OperationID(group.Name, route.Name)
			guard := outcome.FoldOption(keyed.Entry(guarded, operation), func(guard routeGuard) routeGuard {
				return guard
			}, func() routeGuard {
				return func(_ *http.Request, next func() nocontract.Answer) nocontract.Answer { return next() }
			})
			handle := endpointAnswer(operation, route, guard, requests, answers, dispatcher)
			endpoints = append(endpoints, nocontract.Endpoint{Method: route.Method.Name(), Path: routePath(route.Path), Answer: handle})
		}
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		nocontract.Respond(writer, request, endpoints, layers)
	})
}

func routePath(segments []description.PathSegment) []nocontract.PathSegment {
	path := make([]nocontract.PathSegment, 0, len(segments))
	for _, segment := range segments {
		switch segment := segment.(type) {
		case description.PathText:
			path = append(path, nocontract.PathText{Text: segment.Text})
		case description.PathParameter:
			path = append(path, nocontract.PathParameter{Name: segment.Field.Name.Text()})
		case nil:
		}
	}
	return path
}

func endpointAnswer(operation string, route description.Endpoint, guard routeGuard, requests, responses bodyReaders,
	dispatcher Dispatcher,
) func(*http.Request) nocontract.Answer {
	read := routeInput(route, requests)
	answers := compileAnswers(responses, slices.Concat(route.Successes.Members(), route.Failures))
	return func(request *http.Request) nocontract.Answer {
		return guard(request, func() nocontract.Answer {
			return read(request, bodyObject{}, func(fields bodyObject) nocontract.Answer {
				return outcome.Fold(dispatcher.call(request.Context(), invocation{Operation: operation, Input: fields}), func(reply reply) nocontract.Answer {
					switch reported := reply.(type) {
					case answered:
						return encodeMember(answers, reported)
					case admitted:
						return unwrittenAnswer(operation, routeAdmitted{Operation: operation})
					case nil:
					}
					return unwrittenAnswer(operation, replyMissing{Operation: operation})
				}, func(cause error) nocontract.Answer { return unwrittenAnswer(operation, cause) })
			})
		})
	}
}

func guardRequest(operation string, route GuardedRoute, bodies bodyReaders, dispatcher Dispatcher) routeGuard {
	answers := compileAnswers(bodies, route.Refusals)
	return func(request *http.Request, next func() nocontract.Answer) nocontract.Answer {
		headers := make(map[string]string, len(request.Header))
		for name := range request.Header {
			headers[strings.ToLower(name)] = request.Header.Get(name)
		}
		input := guardInput{Scope: route.Scope, Headers: headers}
		return outcome.Fold(dispatcher.call(request.Context(), invocation{Operation: operation, Input: input}), func(reply reply) nocontract.Answer {
			switch reported := reply.(type) {
			case answered:
				return encodeMember(answers, reported)
			case admitted:
				return next()
			case nil:
			}
			return unwrittenAnswer(operation, replyMissing{Operation: operation})
		}, func(cause error) nocontract.Answer { return unwrittenAnswer(operation, cause) })
	}
}

//ergo:not-an-error
type guardInput struct {
	Scope   string            `json:"scope"`
	Headers map[string]string `json:"headers"`
}

func compileAnswers(bodies bodyReaders, members []description.Member) memberAnswers {
	answers := make(memberAnswers, len(members))
	for _, member := range members {
		read := recordBody(bodies, member.Fields)
		answers[member.Name] = func(fields json.RawMessage) nocontract.Answer {
			switch read := nocontract.ReadJSONBody(bytes.NewReader(fields), read).(type) {
			case nocontract.Read[map[string]json.RawMessage]:
				return memberAnswer(member, read.Value)
			case nocontract.BodyRefused:
				return unwrittenAnswer(member.Name, read.Cause)
			case nil:
			}
			return unwrittenAnswer(member.Name, answerFieldsUnread{Member: member.Name})
		}
	}
	return answers
}

func encodeMember(answers memberAnswers, reported answered) nocontract.Answer {
	return outcome.FoldOption(keyed.Entry(answers, reported.Tag), func(answer func(json.RawMessage) nocontract.Answer) nocontract.Answer {
		return answer(reported.Fields)
	}, func() nocontract.Answer {
		return unwrittenAnswer(reported.Tag, answerMemberUndeclared{Tag: reported.Tag})
	})
}

func memberAnswer(member description.Member, fields map[string]json.RawMessage) nocontract.Answer {
	switch answer := member.Answer.(type) {
	case description.Succeeds:
		return nocontract.Succeeded(nocontract.Success{Status: answer.Status.Code(), Tag: member.Name}, fields, nocontract.AnswerMarshalers{})
	case description.Fails:
		return nocontract.Failed(nocontract.Failure{Status: answer.Status.Code(), Tag: member.Name, Code: answer.Code.String()}, fields, nocontract.AnswerMarshalers{})
	case nil:
	}
	return unwrittenAnswer(member.Name, answerStatusMissing{Tag: member.Name})
}

func unwrittenAnswer(answer string, cause error) nocontract.Answer {
	return nocontract.NocontractAnswerSharedFailure(nocontract.AnswerNotWritten{Answer: answer, Reason: cause.Error()})
}

func unreadParameter() readParameter {
	return func(_ nocontract.Text, _ func(json.RawMessage) nocontract.Answer) nocontract.Answer {
		return unwrittenAnswer("request parameter", parameterSchemaMissing{})
	}
}
