package assembling

import (
	"maps"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/outcome"
	"github.com/samber/mo"

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

// pathRefused is a path with a segment nothing reads, a field reading a segment it does not have,
// or a segment with no client form.
//
//ergo:not-an-error
type pathRefused struct{ refusals []Refusal }

func (pathBound) isPathBinding()   {}
func (pathRefused) isPathBinding() {}

// boundPath binds each {name} segment of the route's path to the field reading name.
func boundPath(declared DeclaredEndpoint) pathBinding {
	unbound := map[string]description.Field{}
	for _, field := range declared.Params {
		unbound[field.Name] = field
	}
	segments, refusals := []description.PathSegment{}, []Refusal{}
	refused := func(problem Problem) {
		refusals = append(refusals, Refusal{Position: declared.Position, Problem: problem})
	}
	for _, text := range strings.Split(strings.TrimPrefix(declared.Path, "/"), "/") {
		switch segment := parsedSegment(text).(type) {
		case literalSegment:
			segments = append(segments, description.PathText{Text: text})
		case parameterSegment:
			outcome.FoldOption(keyed.Entry(unbound, segment.name), func(field description.Field) struct{} {
				delete(unbound, segment.name)
				segments = append(segments, description.PathParameter{Field: field})
				return struct{}{}
			}, func() struct{} {
				refused(PathParameterUndeclared{Parameter: segment.name})
				return struct{}{}
			})
		case unsupportedSegment:
			refused(PathSegmentUnsupported{Segment: text})
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

// segmentRead is one segment of a path read as text, a parameter, or a form the client cannot hold.
//
//sumtype:decl
type segmentRead interface{ isSegmentRead() }

// literalSegment is a segment holding no brace.
//
//ergo:not-an-error
type literalSegment struct{}

// parameterSegment is a segment that is one {name}, and the name.
//
//ergo:not-an-error
type parameterSegment struct{ name string }

// unsupportedSegment is a {name...} or {$} wildcard, or a brace that does not enclose the whole
// segment.
//
//ergo:not-an-error
type unsupportedSegment struct{}

func (literalSegment) isSegmentRead()     {}
func (parameterSegment) isSegmentRead()   {}
func (unsupportedSegment) isSegmentRead() {}

// parsedSegment reads one segment of a path: one {name}, text holding no brace, or neither.
func parsedSegment(text string) segmentRead {
	braced := mo.TupleToOption(strings.CutPrefix(text, "{")).FlatMap(func(inner string) mo.Option[string] {
		return mo.TupleToOption(strings.CutSuffix(inner, "}"))
	})
	return mo.Fold(braced, parameterNamed, func(error) segmentRead {
		if strings.ContainsAny(text, "{}") {
			return unsupportedSegment{}
		}
		return literalSegment{}
	})
}

// parameterNamed is a segment that is one {name}: a parameter, or a wildcard or empty name the
// client has no form for.
func parameterNamed(name string) segmentRead {
	switch {
	case name == "", name == "$", strings.HasSuffix(name, "..."), strings.ContainsAny(name, "{}"):
		return unsupportedSegment{}
	}
	return parameterSegment{name: name}
}
