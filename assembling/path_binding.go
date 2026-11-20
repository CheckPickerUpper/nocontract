package assembling

import (
	"fmt"
	"maps"
	"slices"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

// pathBinding is a route's path with each parameter bound to the field that reads it, or every
// reason it cannot be.
//
//sumtype:decl
type pathBinding interface{ isPathBinding() }

// pathBound is a path whose every parameter one field reads, and that holds no other field.
//
//ergo:not-an-error
type pathBound struct{ segments []description.PathSegment }

// pathRefused is a path with a parameter nothing reads, or a field reading a parameter the path
// does not have.
//
//ergo:not-an-error
type pathRefused struct{ refusals []Refusal }

func (pathBound) isPathBinding()   {}
func (pathRefused) isPathBinding() {}

// boundPath binds each parameter of the route's path to the field reading it.
func boundPath(declared DeclaredEndpoint) pathBinding {
	unbound := map[string]description.Field{}
	for _, field := range declared.Params {
		unbound[field.Name.Text()] = field
	}
	segments, refusals := []description.PathSegment{}, []Refusal{}
	refused := func(problem Problem) {
		refusals = append(refusals, Refusal{Position: declared.Position, Problem: problem})
	}
	for _, segment := range declared.Path.Segments() {
		switch each := segment.(type) {
		case description.RouteText:
			segments = append(segments, description.PathText{Text: each.Text()})
		case description.RouteParameter:
			outcome.FoldOption(keyed.Entry(unbound, each.Name()), func(field description.Field) struct{} {
				delete(unbound, each.Name())
				outcome.FoldOption(description.ParseParameter(field), func(parameter description.Parameter) struct{} {
					segments = append(segments, description.PathParameter{Field: parameter})
					return struct{}{}
				}, func() struct{} {
					refused(parameterNotReadable(field, "path"))
					return struct{}{}
				})
				return struct{}{}
			}, func() struct{} {
				refused(PathParameterUndeclared{Parameter: each.Name()})
				return struct{}{}
			})
		case nil:
		}
	}
	for _, name := range slices.Sorted(maps.Keys(unbound)) {
		refused(PathFieldUnused{Parameter: name})
	}
	if len(refusals) > 0 {
		return pathRefused{refusals: refusals}
	}
	return pathBound{segments: segments}
}

func parameterNotReadable(field description.Field, part string) URLParameterUnsupported {
	return URLParameterUnsupported{Part: part, Field: field.Name.Text(), Schema: fmt.Sprintf("%T", field.Schema)}
}
