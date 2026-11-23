package main

import (
	"log/slog"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

func observeRequest(name string, logger *slog.Logger) nocontract.Layer {
	return func(visit nocontract.Visit, next func() nocontract.Answered) nocontract.Answered {
		fields := []slog.Attr{
			slog.String("layer", name), slog.String("method", visit.Method()),
			slog.String("path", visit.URL().Path), slog.String("route", observedRoute(visit)),
		}
		logger.LogAttrs(visit.Context(), slog.LevelInfo, "layer", append(fields, slog.String("phase", "before"))...)
		answered := next()
		logger.LogAttrs(visit.Context(), slog.LevelInfo, "layer", append(fields,
			slog.String("phase", "after"), slog.Int("status", answered.Status()), slog.Any("code", answered.Code()))...)
		return answered
	}
}

func observedRoute(visit nocontract.Visit) string {
	switch route := visit.Route().(type) {
	case nocontract.RouteMatched:
		return route.Path
	case nocontract.NoRouteMatched, nil:
	}
	return visit.URL().Path
}
