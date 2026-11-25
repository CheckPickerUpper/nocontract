package nocontract

import (
	"log/slog"
	"runtime/debug"

	"github.com/CheckPickerUpper/outcome"
)

// recoverRequest answers RequestCrashed when dispatch panics, and logs the crash through the
// listener's request logger when it has one.
func recoverRequest(visit Visit, logger outcome.Option[*slog.Logger], dispatch func() Answered) Answered {
	method, path, route := visit.Method(), visit.URL().Path, loggedRoute(visit)
	answered := Answered{}
	func() {
		defer func() {
			switch cause := recover().(type) {
			case nil:
			default:
				answered = crashAnswer(visit.Route())
				outcome.ForEachOption(logger, func(logger *slog.Logger) {
					logger.LogAttrs(visit.Context(), slog.LevelError, "http request crashed",
						slog.String("method", method), slog.String("path", path), slog.String("route", route),
						slog.Any("cause", cause), slog.String("stack", string(debug.Stack())))
				})
			}
		}()
		answered = dispatch()
	}()
	return answered
}

func crashAnswer(route VisitedRoute) Answered {
	selected := outcome.None[RequestCrashedRoute]()
	switch matched := route.(type) {
	case RouteMatched:
		selected = outcome.Some(RequestCrashedRoute{Method: matched.Method, Path: matched.Path})
	case NoRouteMatched, nil:
	}
	return Answered{response: answerResponse{answer: sent(NocontractAnswerSharedFailure(RequestCrashed{Route: selected}))}}
}
