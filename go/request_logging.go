package nocontract

import (
	"log/slog"
	"time"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"
)

// WithRequestLogger chooses the application's logger for the listener's request events.
// Each event records route, method, status, duration_us, and the answer's code when present.
func WithRequestLogger(options ListenOptions, logger *slog.Logger) ListenOptions {
	return ListenOptions{address: options.address, requestLogger: outcome.EmptyableToOption(logger), deadline: options.deadline, bodyLimit: options.bodyLimit}
}

func logRequest(logger *slog.Logger, visit Visit, answered Answered, duration time.Duration) {
	route := visit.request.URL.Path
	switch matched := visit.Route().(type) {
	case RouteMatched:
		route = matched.Path
	case NoRouteMatched, nil:
	}
	code := mo.Fold(answered.Code(), func(code string) slog.Attr {
		return slog.String("code", code)
	}, func(error) slog.Attr { return slog.Attr{Key: "", Value: slog.Value{}} })
	logger.LogAttrs(visit.Context(), slog.LevelInfo, "http request",
		slog.String("route", route),
		slog.String("method", visit.Method()),
		slog.Int("status", answered.Status()),
		slog.Int64("duration_us", duration.Microseconds()),
		code,
	)
}
