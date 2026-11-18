package description

import (
	"slices"
	"strings"

	ergo "github.com/CheckPickerUpper/ErGo"

	"github.com/CheckPickerUpper/nocontract/messages"
)

// RoutePath is a route's path as its directive writes it: the segments between its slashes, each
// text or one named parameter. Only ParseRoutePath makes one.
type RoutePath struct{ segments []RouteSegment }

// Segments is each segment of the path, in order.
func (path RoutePath) Segments() []RouteSegment { return slices.Clone(path.segments) }

// Text is the path as the directive writes it.
func (path RoutePath) Text() string {
	written := []string{}
	for _, segment := range path.segments {
		switch each := segment.(type) {
		case RouteText:
			written = append(written, each.text)
		case RouteParameter:
			written = append(written, "{"+each.name+"}")
		case nil:
		}
	}
	return "/" + strings.Join(written, "/")
}

// Matched is the path as the requests it matches see it: each parameter written as {}, so two
// paths that differ only in their parameters' names are one.
func (path RoutePath) Matched() string {
	written := []string{}
	for _, segment := range path.segments {
		switch each := segment.(type) {
		case RouteText:
			written = append(written, each.text)
		case RouteParameter:
			written = append(written, "{}")
		case nil:
		}
	}
	return "/" + strings.Join(written, "/")
}

// RouteSegment is one segment of a route's path.
//
//sumtype:decl
type RouteSegment interface{ isRouteSegment() }

// RouteText is a segment a request's path must hold as written: text holding no brace.
//
//ergo:not-an-error
type RouteText struct{ text string }

// Text is the segment's text.
func (segment RouteText) Text() string { return segment.text }

// RouteParameter is a segment that is one {name}, taking any text a request's path holds there.
//
//ergo:not-an-error
type RouteParameter struct{ name string }

// Name is the parameter's name, which the field reading it carries.
func (segment RouteParameter) Name() string { return segment.name }

func (RouteText) isRouteSegment()      {}
func (RouteParameter) isRouteSegment() {}

// RoutePathReading is what reading a directive's text as a route's path ended as.
//
//sumtype:decl
type RoutePathReading interface{ isRoutePathReading() }

// RoutePathRead is a text that is a route's path.
//
//ergo:not-an-error
type RoutePathRead struct{ Path RoutePath }

// RoutePathRefused is a text that is not a route's path, and why.
//
//ergo:not-an-error
type RoutePathRefused struct{ Problem RoutePathProblem }

func (RoutePathRead) isRoutePathReading()    {}
func (RoutePathRefused) isRoutePathReading() {}

// RoutePathProblem is why a text is not a route's path, as ErGo describes a failure.
//
//sumtype:decl
type RoutePathProblem interface {
	isRoutePathProblem()
	ErrorCode() ergo.Code
	Report(language messages.Language) ergo.Report
	Causes() []error
}

// PathNotRooted is a route's path that does not start with /.
//
//ergo:code NC-1090
//ergo:message The route's path {Path} does not start with /.
type PathNotRooted struct{ Path string }

// PathParameterUnnamed is a route's path holding {}, a parameter without a name.
//
//ergo:code NC-1091
//ergo:message The route's path {Path} holds a parameter without a name.
type PathParameterUnnamed struct{ Path string }

// PathEndMatched is a route's path holding {$}, which ServeMux reads as the path's end and the
// client cannot write.
//
//ergo:code NC-1092
//ergo:message The route's path {Path} holds the end-of-path wildcard $; a route's path ends where its last segment does.
type PathEndMatched struct{ Path string }

// PathRestMatched is a route's path holding {name...}, a parameter taking every segment left, which
// the client cannot write.
//
//ergo:code NC-1093
//ergo:message The route's path {Path} holds {Segment}, which takes every segment left; a parameter takes one segment.
type PathRestMatched struct {
	Path    string
	Segment string
}

// PathBraceInName is a route's path with a parameter whose name holds a brace.
//
//ergo:code NC-1094
//ergo:message The route's path {Path} holds {Segment}, a parameter whose name holds a brace.
type PathBraceInName struct {
	Path    string
	Segment string
}

// PathBraceInText is a route's path with a segment holding a brace that does not enclose it whole.
//
//ergo:code NC-1095
//ergo:message The route's path {Path} holds {Segment}; a segment is text holding no brace, or one parameter name in braces.
type PathBraceInText struct {
	Path    string
	Segment string
}

func (PathNotRooted) isRoutePathProblem()        {}
func (PathParameterUnnamed) isRoutePathProblem() {}
func (PathEndMatched) isRoutePathProblem()       {}
func (PathRestMatched) isRoutePathProblem()      {}
func (PathBraceInName) isRoutePathProblem()      {}
func (PathBraceInText) isRoutePathProblem()      {}

// ParseRoutePath reads text as a route's path: / and then segments split by /, each text holding
// no brace or one {name}.
func ParseRoutePath(text string) RoutePathReading {
	rest, isRooted := strings.CutPrefix(text, "/")
	if !isRooted {
		return RoutePathRefused{Problem: PathNotRooted{Path: text}}
	}
	segments := []RouteSegment{}
	for written := range strings.SplitSeq(rest, "/") {
		inner, isOpened := strings.CutPrefix(written, "{")
		name, isClosed := strings.CutSuffix(inner, "}")
		switch {
		case !isOpened || !isClosed:
			if strings.ContainsAny(written, "{}") {
				return RoutePathRefused{Problem: PathBraceInText{Path: text, Segment: written}}
			}
			segments = append(segments, RouteText{text: written})
		case name == "":
			return RoutePathRefused{Problem: PathParameterUnnamed{Path: text}}
		case name == "$":
			return RoutePathRefused{Problem: PathEndMatched{Path: text}}
		case strings.HasSuffix(name, "..."):
			return RoutePathRefused{Problem: PathRestMatched{Path: text, Segment: written}}
		case strings.ContainsAny(name, "{}"):
			return RoutePathRefused{Problem: PathBraceInName{Path: text, Segment: written}}
		default:
			segments = append(segments, RouteParameter{name: name})
		}
	}
	return RoutePathRead{Path: RoutePath{segments: segments}}
}
