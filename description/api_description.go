// Package description is the API a backend serves, described without any one language: its
// routes, what each reads from a request, the members of each route's outcome and every named
// type they use. Each language's reader writes it; the Effect writer renders it.
package description

import (
	"unicode"
	"unicode/utf8"
)

// API is every route a backend serves, grouped by the module of the backend that declares it.
type API struct {
	Name    string
	Groups  []Group
	Members []Member
	Structs []NamedStruct
	Shared  SharedFailures
}

// SharedFailures names the members every route answers before or after its handler runs.
type SharedFailures struct {
	RequestRefused   string
	AnswerNotWritten string
}

// Group is the routes one module of the backend declares, named for the module.
type Group struct {
	Name      string
	Endpoints []Endpoint
}

// GroupClassName is the TypeScript class of the routes a module named name declares.
func GroupClassName(name string) string {
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(first)) + name[size:] + "Group"
}

// Endpoint is one route: its name, its method and path, with {name} path segments, what it reads
// from the request and the members of its outcome, in declaration order.
type Endpoint struct {
	Name    string
	Method  string
	Path    string
	Params  []Field
	Query   []Field
	Body    Body
	Members []string
}

// ReadsRequest says whether the route reads any part of its request, so it can answer the
// shared RequestRefused.
func (endpoint Endpoint) ReadsRequest() bool {
	switch endpoint.Body.(type) {
	case JSONBody:
		return true
	case NoBody, nil:
	}
	return len(endpoint.Params) > 0 || len(endpoint.Query) > 0
}

// Body is what a route reads from its request body.
//
//sumtype:decl
type Body interface{ isBody() }

// NoBody is a route that reads no body.
//
//ergo:not-an-error
type NoBody struct{}

// JSONBody is a route that reads a JSON body of Schema.
//
//ergo:not-an-error
type JSONBody struct{ Schema Schema }

func (NoBody) isBody()   {}
func (JSONBody) isBody() {}

// Member is one member of a route's outcome: its name, which is its tag on the wire, how it
// answers and its JSON fields.
type Member struct {
	Name   string
	Answer Answer
	Fields []Field
}

// Answer is whether a member answers as a success or as a failure with its permanent code.
//
//sumtype:decl
type Answer interface{ isAnswer() }

// Succeeds is a member answered with a success Status.
//
//ergo:not-an-error
type Succeeds struct{ Status int }

// Fails is a member answered with a failure Status and its permanent Code.
//
//ergo:not-an-error
type Fails struct {
	Status int
	Code   string
}

func (Succeeds) isAnswer() {}
func (Fails) isAnswer()    {}

// Field is one JSON field: its name on the wire and its schema.
type Field struct {
	Name   string
	Schema Schema
}

// NamedStruct is a named struct a member or a body uses, written once and referred to.
type NamedStruct struct {
	Name   string
	Fields []Field
}

// Schema is the shape of one JSON value.
//
//sumtype:decl
type Schema interface{ isSchema() }

// Text is a JSON string.
//
//ergo:not-an-error
type Text struct{}

// Integer is a JSON whole number.
//
//ergo:not-an-error
type Integer struct{}

// Finite is a JSON finite number.
//
//ergo:not-an-error
type Finite struct{}

// Truth is a JSON boolean.
//
//ergo:not-an-error
type Truth struct{}

// Instant is a JSON string holding an RFC 3339 time.
//
//ergo:not-an-error
type Instant struct{}

// Literals is a JSON string holding one of Values.
//
//ergo:not-an-error
type Literals struct{ Values []string }

// List is a JSON array of Item.
//
//ergo:not-an-error
type List struct{ Item Schema }

// Dictionary is a JSON object whose keys are strings and whose values are Value.
//
//ergo:not-an-error
type Dictionary struct{ Value Schema }

// Nullable is Inner or null.
//
//ergo:not-an-error
type Nullable struct{ Inner Schema }

// StructRef is the named struct Name.
//
//ergo:not-an-error
type StructRef struct{ Name string }

// InlineStruct is an unnamed struct with Fields.
//
//ergo:not-an-error
type InlineStruct struct{ Fields []Field }

func (Text) isSchema()         {}
func (Integer) isSchema()      {}
func (Finite) isSchema()       {}
func (Truth) isSchema()        {}
func (Instant) isSchema()      {}
func (Literals) isSchema()     {}
func (List) isSchema()         {}
func (Dictionary) isSchema()   {}
func (Nullable) isSchema()     {}
func (StructRef) isSchema()    {}
func (InlineStruct) isSchema() {}
