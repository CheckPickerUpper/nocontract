package nocontract_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"testing"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/outcome"
)

type panicWitness struct {
	reason string
}

func crashWithPanic() nocontract.Answer {
	panic(panicWitness{reason: "request panic"})
}

func crashWithBounds(answers []nocontract.Answer, index int) nocontract.Answer {
	return answers[index]
}

func TestRequestCrashDiagnosticsKeepTheNativeCauseStackAndRequest(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name     string
		frame    string
		dispatch func() nocontract.Answer
		cause    func(*testing.T, any)
	}{
		{
			name: "panic", frame: "crashWithPanic", dispatch: crashWithPanic,
			cause: func(t *testing.T, cause any) {
				t.Helper()
				witness := requireNativeCrashCause[panicWitness](t, cause)
				if witness.reason != "request panic" {
					t.Fatalf("native panic lost its reason: %#v", witness)
				}
			},
		},
		{
			name: "bounds", frame: "crashWithBounds", dispatch: func() nocontract.Answer { return crashWithBounds([]nocontract.Answer{}, 1) },
			cause: func(t *testing.T, cause any) { t.Helper(); _ = requireNativeCrashCause[runtime.Error](t, cause) },
		},
	} {
		t.Run(example.name, func(t *testing.T) {
			t.Parallel()
			records := make(chan slog.Record, 1)
			logger := slog.New(crashLogRecorder{destination: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}), records: records})
			binding := nocontract.BindRoutes([]nocontract.Endpoint{{
				Method: http.MethodPost,
				Path:   []nocontract.PathSegment{nocontract.PathText{Text: "orders"}, nocontract.PathParameter{Name: "order_id"}},
				Answer: func(request *http.Request, _ nocontract.RequestBodyLimit) nocontract.Answer {
					request.Method = http.MethodDelete
					request.URL.Path = "/changed"
					return example.dispatch()
				},
			}}, []nocontract.Layer{})
			options := nocontract.WithRequestLogger(nocontract.ListenOn("127.0.0.1:0"), logger)
			listening := outcome.Fold(nocontract.Listen(t.Context(), options, binding), func(listening nocontract.ListeningServer) nocontract.ListeningServer { return listening }, func(cause error) nocontract.ListeningServer { t.Fatal(cause); return nocontract.ListeningServer{} })
			t.Cleanup(func() {
				outcome.Fold(listening.Stop(context.WithoutCancel(t.Context())), func(nocontract.ServerStopped) struct{} { return struct{}{} }, func(cause error) struct{} { t.Error(cause); return struct{}{} })
			})
			request := outcome.Fold(outcome.TupleToResult(http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+listening.Address()+"/orders/7", http.NoBody)), func(request *http.Request) *http.Request { return request }, func(cause error) *http.Request { t.Fatal(cause); return &http.Request{} })
			outcome.Fold(outcome.TupleToResult(http.DefaultClient.Do(request)), func(response *http.Response) struct{} {
				defer func() {
					outcome.ForEachOption(outcome.EmptyableToOption(response.Body.Close()), func(cause error) { t.Error(cause) })
				}()
				if response.StatusCode != 500 {
					t.Fatalf("crash HTTP status = %d", response.StatusCode)
				}
				return struct{}{}
			}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
			select {
			case record := <-records:
				if crashAttribute(t, record, "method").String() != http.MethodPost || crashAttribute(t, record, "path").String() != "/orders/7" || crashAttribute(t, record, "route").String() != "/orders/{order_id}" {
					t.Fatalf("crash log lost the original request: %v", record)
				}
				stack := crashAttribute(t, record, "stack").String()
				if !bytes.Contains([]byte(stack), []byte(example.frame)) {
					t.Fatalf("stack lost %s:\n%s", example.frame, stack)
				}
				example.cause(t, crashAttribute(t, record, "cause").Any())
			default:
				t.Fatal("the listener answered without a crash diagnostic")
			}
		})
	}
}

type crashLogRecorder struct {
	destination slog.Handler
	records     chan slog.Record
}

func (recorder crashLogRecorder) Enabled(requestContext context.Context, level slog.Level) bool {
	return recorder.destination.Enabled(requestContext, level)
}
func (recorder crashLogRecorder) Handle(requestContext context.Context, record slog.Record) error {
	if record.Message == "http request crashed" {
		recorder.records <- record.Clone()
	}
	return recorder.destination.Handle(requestContext, record)
}
func (recorder crashLogRecorder) WithAttrs(attributes []slog.Attr) slog.Handler {
	return crashLogRecorder{destination: recorder.destination.WithAttrs(attributes), records: recorder.records}
}
func (recorder crashLogRecorder) WithGroup(name string) slog.Handler {
	return crashLogRecorder{destination: recorder.destination.WithGroup(name), records: recorder.records}
}

func crashAttribute(t *testing.T, record slog.Record, name string) slog.Value {
	t.Helper()
	value := outcome.None[slog.Value]()
	record.Attrs(func(attribute slog.Attr) bool {
		if attribute.Key == name {
			value = outcome.Some(attribute.Value)
		}
		return true
	})
	return outcome.FoldOption(value, func(value slog.Value) slog.Value { return value }, func() slog.Value { t.Fatalf("crash log has no %s attribute", name); return slog.Value{} })
}

func requireNativeCrashCause[Cause any](t *testing.T, cause any) Cause {
	t.Helper()
	native, matched := cause.(Cause)
	if !matched {
		t.Fatalf("native cause became %T (%v); want %T", cause, cause, native)
	}
	return native
}
