package running_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/CheckPickerUpper/nocontract/internal/scratchproject"
	"github.com/CheckPickerUpper/outcome"
)

type shopAnsweredRequest struct {
	request shopRequestCase
	answer  shopAnswer
}

type shopObservation struct {
	Layer      string          `json:"layer"`
	Phase      string          `json:"phase"`
	Method     string          `json:"method"`
	Path       string          `json:"path"`
	Route      string          `json:"route"`
	Status     int             `json:"status"`
	Code       json.RawMessage `json:"code"`
	DurationUS json.RawMessage `json:"duration_us"`
}

func observedShopCode(code json.RawMessage) string {
	switch string(code) {
	case "", "null":
		return ""
	}
	return string(code)
}

func readShopObservations(t *testing.T, lines []string) []shopObservation {
	t.Helper()
	entries := []shopObservation{}
	for _, line := range lines {
		brace := strings.IndexByte(line, '{')
		if brace < 0 {
			continue
		}
		message := []byte(line[brace:])
		if !json.Valid(message) {
			continue
		}
		entry := shopObservation{Layer: "", Phase: "", Method: "", Path: "", Route: "", Status: 0, Code: json.RawMessage{}, DurationUS: json.RawMessage{}}
		scratchproject.StopOn(t, "reading shop observation "+line, json.Unmarshal(message, &entry))
		if entry.Route != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func assertShopObservations(t *testing.T, contract shopContract, requests []shopAnsweredRequest, lines []string) {
	t.Helper()
	entries := readShopObservations(t, lines)
	if len(entries) < len(requests)*5 || len(entries)%5 != 0 {
		t.Fatalf("got %d layer/log observations for %d checked HTTP answers; want four layer observations and one request log per answer:\n%s", len(entries), len(requests), strings.Join(lines, "\n"))
	}
	for index := range len(entries) / 5 {
		group := entries[index*5 : index*5+5]
		visit, logged := group[0], group[4]
		for position, expected := range []struct{ layer, phase string }{
			{layer: "outer", phase: "before"}, {layer: "inner", phase: "before"},
			{layer: "inner", phase: "after"}, {layer: "outer", phase: "after"},
		} {
			entry := group[position]
			if entry.Layer != expected.layer || entry.Phase != expected.phase || entry.Method != visit.Method || entry.Path != visit.Path || entry.Route != visit.Route {
				t.Fatalf("request %d layer %d = %+v; want %s %s observing %+v", index, position, entry, expected.layer, expected.phase, visit)
			}
			if expected.phase == "after" && (entry.Status != logged.Status || observedShopCode(entry.Code) != observedShopCode(logged.Code)) {
				t.Fatalf("request %d layer answer %+v differs from request log %+v", index, entry, logged)
			}
		}
		if logged.Layer != "" || logged.Phase != "" || logged.Method != visit.Method || logged.Route != visit.Route {
			t.Fatalf("request %d final event = %+v; want one request log for %+v", index, logged, visit)
		}
		assertShopLoggedDuration(t, index, logged)
		if index >= len(requests) {
			continue
		}
		answered := requests[index]
		assertShopLogMatchesAnswer(t, contract, answered, logged)
		path, _, _ := strings.Cut(answered.request.path, "?")
		if visit.Method != answered.request.method || visit.Path != path || visit.Route != logged.Route {
			t.Fatalf("%s: visit %+v and log %+v disagree with HTTP %d %s", answered.request.name, visit, logged, answered.answer.status, answered.answer.body)
		}
	}
	t.Logf("observed %d HTTP answers: outer before, inner before, inner after, outer after, one final request log; status and code match the returned answers", len(entries)/5)
}

func assertShopLoggedDuration(t *testing.T, index int, logged shopObservation) {
	t.Helper()
	outcome.Fold(outcome.TupleToResult(strconv.ParseInt(string(logged.DurationUS), 10, 64)), func(duration int64) struct{} {
		if duration < 0 {
			t.Errorf("request %d duration_us = %d; want nonnegative", index, duration)
		}
		return struct{}{}
	}, func(cause error) struct{} { t.Errorf("request %d duration_us: %v", index, cause); return struct{}{} })
}

func assertShopLogMatchesAnswer(t *testing.T, contract shopContract, answered shopAnsweredRequest, logged shopObservation) {
	t.Helper()
	path, _, _ := strings.Cut(answered.request.path, "?")
	route := outcome.FoldOption(matchShopOperation(contract, answered.request), func(matched shopOperationMatch) string { return matched.path }, func() string { return path })
	body := struct {
		Code json.RawMessage `json:"code"`
	}{Code: json.RawMessage{}}
	if answered.answer.body != "" {
		scratchproject.StopOn(t, "reading the actual HTTP answer", json.Unmarshal([]byte(answered.answer.body), &body))
	}
	if logged.Method != answered.request.method || logged.Route != route || logged.Status != answered.answer.status || observedShopCode(logged.Code) != observedShopCode(body.Code) {
		t.Fatalf("%s: log %+v disagrees with HTTP %d %s (route %s)", answered.request.name, logged, answered.answer.status, answered.answer.body, route)
	}
}
