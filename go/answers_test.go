package nocontract

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func written(t *testing.T, answer Answer) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	sent(answer).write(recorder)
	return recorder.Code, recorder.Body.String()
}

type queued struct {
	Reference string `json:"reference"`
}

type emptied struct{}

type missing struct {
	ID int `json:"id"`
}

type fed struct {
	Feed chan int `json:"feed"`
}

// custom writes itself with a method of its own, which the description of its fields does not
// read, so it is answered with the fields alone.
type custom struct {
	ID int `json:"id"`
}

func (custom) MarshalJSON() ([]byte, error) { return []byte(`{"custom":true}`), nil }

func TestAnAnswerIsOneObjectOfItsTagCodeAndFields(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		answer Answer
		status int
		body   string
	}{
		{
			answer: Succeeded(Success{Status: http.StatusAccepted, Tag: "Queued"}, queued{Reference: "r1"}, AnswerMarshalers{}), status: http.StatusAccepted,
			body: `{"_tag":"Queued","reference":"r1"}`,
		},
		{answer: Succeeded(Success{Status: http.StatusOK, Tag: "Empty"}, emptied{}, AnswerMarshalers{}), status: http.StatusOK, body: `{"_tag":"Empty"}`},
		{
			answer: Failed(Failure{Status: http.StatusNotFound, Tag: "Missing", Code: "AB-1001"}, missing{ID: 7}, AnswerMarshalers{}), status: http.StatusNotFound,
			body: `{"_tag":"Missing","code":"AB-1001","id":7}`,
		},
		{answer: Succeeded(Success{Status: http.StatusOK, Tag: "Custom"}, custom{ID: 3}, AnswerMarshalers{}), status: http.StatusOK, body: `{"_tag":"Custom","id":3}`},
		{
			answer: Succeeded(Success{Status: http.StatusOK, Tag: "Fed"}, fed{Feed: make(chan int)}, AnswerMarshalers{}), status: http.StatusInternalServerError,
			body: `{"_tag":"AnswerNotWritten","code":"NC-1002","answer":"Fed","reason":"json: unsupported type: chan int"}`,
		},
		{answer: NoOutcome(), status: http.StatusInternalServerError, body: `{"_tag":"AnswerNotWritten","code":"NC-1002","answer":"","reason":"the route answered no outcome"}`},
	} {
		status, body := written(t, each.answer)
		if status != each.status || body != each.body {
			t.Errorf("wrote %d %s; want %d %s", status, body, each.status, each.body)
		}
		if !json.Valid([]byte(body)) {
			t.Errorf("wrote %s, which is not JSON", body)
		}
	}
}
