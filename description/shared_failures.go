package description

import (
	"net/http"

	ergo "github.com/CheckPickerUpper/ErGo"
)

// SharedFailure is a failure nocontract answers on a route's behalf, outside its handler, declared
// once for every backend language: its name, the failure status and permanent code it answers
// with, its JSON fields, which routes can answer it, what its type's documentation says, and its
// message. Each runtime's own declaration of it is generated from this one.
type SharedFailure struct {
	Name    string
	Status  FailureStatus
	Code    ergo.Code
	Fields  []Field
	Answers SharedAnswer
	Doc     string
	Message []MessagePart
}

// Member is the failure as every client declares it.
func (failure SharedFailure) Member() Member {
	return Member{Name: failure.Name, Answer: Fails{Status: failure.Status, Code: failure.Code}, Fields: failure.Fields}
}

// SharedAnswer is which routes can answer a shared failure.
//
//sumtype:decl
type SharedAnswer interface{ isSharedAnswer() }

// AnsweredWhenRequestRead is a failure every route whose input reads a path segment, a query
// parameter or a body can answer, before its handler runs.
//
//ergo:not-an-error
type AnsweredWhenRequestRead struct{}

// AnsweredByEveryRoute is a failure every route can answer.
//
//ergo:not-an-error
type AnsweredByEveryRoute struct{}

// AnsweredWithoutRoute is a failure the server answers to a request no declared route takes, so no
// route lists it.
//
//ergo:not-an-error
type AnsweredWithoutRoute struct{}

func (AnsweredWhenRequestRead) isSharedAnswer() {}
func (AnsweredByEveryRoute) isSharedAnswer()    {}
func (AnsweredWithoutRoute) isSharedAnswer()    {}

// MessagePart is one piece of a shared failure's message: text, written as it is, or the value of
// one of its fields, named as it is on the wire. Each runtime spells the field its own way.
//
//sumtype:decl
type MessagePart interface{ isMessagePart() }

// MessageText is text the message holds as it is. It holds no brace, which a message format reads
// as the start or end of an argument.
//
//ergo:not-an-error
type MessageText struct{ Text string }

// MessageField is the value of the failure's field named Field on the wire.
//
//ergo:not-an-error
type MessageField struct{ Field string }

func (MessageText) isMessagePart()  {}
func (MessageField) isMessagePart() {}

// SharedFailures is every failure nocontract answers on a route's behalf, in the order each route
// lists them.
func SharedFailures() []SharedFailure {
	return []SharedFailure{
		{
			Name: "RequestRefused", Status: FailureStatus{code: http.StatusBadRequest}, Code: "NC-1001",
			Fields: []Field{
				{Name: FieldName{text: "part"}, Schema: Literals{Values: []string{"path", "query", "body"}}},
				{Name: FieldName{text: "name"}, Schema: Text{}},
				{Name: FieldName{text: "reason"}, Schema: Text{}},
			},
			Answers: AnsweredWhenRequestRead{},
			Doc: "RequestRefused is a request whose path segment, query parameter or body the route's input could " +
				"not read. Every route that reads its request can answer it, before its handler runs.",
			Message: []MessagePart{
				MessageText{Text: "The request's "},
				MessageField{Field: "part"},
				MessageText{Text: " "},
				MessageField{Field: "name"},
				MessageText{Text: " could not be read: "},
				MessageField{Field: "reason"},
			},
		},
		{
			Name: "AnswerNotWritten", Status: FailureStatus{code: http.StatusInternalServerError}, Code: "NC-1002",
			Fields:  []Field{{Name: FieldName{text: "answer"}, Schema: Text{}}, {Name: FieldName{text: "reason"}, Schema: Text{}}},
			Answers: AnsweredByEveryRoute{},
			Doc: "AnswerNotWritten is a route whose outcome could not be written as JSON, or that answered no " +
				"outcome at all. Every route can answer it.",
			Message: []MessagePart{MessageText{Text: "The route could not write its answer: "}, MessageField{Field: "reason"}},
		},
		{
			Name: "RouteNotFound", Status: FailureStatus{code: http.StatusNotFound}, Code: "NC-1069",
			Fields:  []Field{{Name: FieldName{text: "method"}, Schema: Text{}}, {Name: FieldName{text: "path"}, Schema: Text{}}},
			Answers: AnsweredWithoutRoute{},
			Doc:     "RouteNotFound is a request to a path no route declares.",
			Message: []MessagePart{
				MessageText{Text: "No route is declared at "},
				MessageField{Field: "path"},
				MessageText{Text: ", asked for with "},
				MessageField{Field: "method"},
			},
		},
		{
			Name: "MethodNotDeclared", Status: FailureStatus{code: http.StatusMethodNotAllowed}, Code: "NC-1070",
			Fields:  []Field{{Name: FieldName{text: "method"}, Schema: Text{}}, {Name: FieldName{text: "allowed"}, Schema: List{Item: Text{}}}},
			Answers: AnsweredWithoutRoute{},
			Doc: "MethodNotDeclared is a request to a declared path with a method no route at that path declares, " +
				"and the methods the path's routes declare.",
			Message: []MessagePart{MessageText{Text: "No route at this path answers "}, MessageField{Field: "method"}},
		},
	}
}
