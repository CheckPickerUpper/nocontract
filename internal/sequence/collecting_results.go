// Package sequence collects typed reads while preserving the first refusal.
package sequence

import "github.com/CheckPickerUpper/outcome"

// Traverse reads each value in order and returns every result, or the first refusal.
func Traverse[A, B any](values []A, read func(A) outcome.Result[B]) outcome.Result[[]B] {
	collected := outcome.Ok([]B{})
	for _, value := range values {
		collected = outcome.Fold(collected, func(previous []B) outcome.Result[[]B] {
			return outcome.Fold(read(value), func(next B) outcome.Result[[]B] { return outcome.Ok(append(previous, next)) }, outcome.Err[[]B])
		}, outcome.Err[[]B])
	}
	return collected
}
