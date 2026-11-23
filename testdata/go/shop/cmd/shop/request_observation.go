package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/CheckPickerUpper/outcome"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

func observeRequest(name string, logger *slog.Logger) nocontract.Layer {
	holdAddress := outcome.None[string]()
	if name == "outer" {
		holdAddress = outcome.EmptyableToOption(os.Getenv("SHOP_HOLD_REQUEST_ADDRESS"))
	}
	return func(visit nocontract.Visit, next func() nocontract.Answered) nocontract.Answered {
		fields := []slog.Attr{
			slog.String("layer", name), slog.String("method", visit.Method()),
			slog.String("path", visit.URL().Path), slog.String("route", observedRoute(visit)),
		}
		logger.LogAttrs(visit.Context(), slog.LevelInfo, "layer", append(fields, slog.String("phase", "before"))...)
		if visit.Method() == http.MethodPost && visit.URL().Path == "/shoppers/7/top-ups" {
			outcome.ForEachOption(holdAddress, func(address string) {
				outcome.Fold(holdShopRequest(visit.Context(), address), func(requestReleased) struct{} { return struct{}{} }, func(cause error) struct{} {
					logger.ErrorContext(visit.Context(), "holding the shop request", slog.Any("cause", cause))
					return struct{}{}
				})
			})
		}
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
