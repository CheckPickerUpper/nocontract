package openapi

import "encoding/json"

// document is an OpenAPI 3.1 document as encoding/json writes it.
type document struct {
	OpenAPI    string                          `json:"openapi"`
	Info       info                            `json:"info"`
	Tags       []tag                           `json:"tags"`
	Paths      map[string]map[string]operation `json:"paths"`
	Components components                      `json:"components"`
}

// info names the API and its own version.
type info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

// tag is a group of routes, by name.
type tag struct {
	Name string `json:"name"`
}

// components holds every schema a reference names.
type components struct {
	Schemas map[string]schema `json:"schemas"`
}

// operation is one route under its path and method: one that reads a JSON body, or one that reads
// none.
//
//sumtype:decl
type operation interface{ isOperation() }

// bodylessOperation is a route that reads no body.
//
//ergo:not-an-error
type bodylessOperation struct {
	Tags        []string            `json:"tags"`
	OperationID string              `json:"operationId"`
	Parameters  []parameter         `json:"parameters"`
	Responses   map[string]response `json:"responses"`
}

// bodyOperation is a route that reads a JSON body.
//
//ergo:not-an-error
type bodyOperation struct {
	Tags        []string            `json:"tags"`
	OperationID string              `json:"operationId"`
	Parameters  []parameter         `json:"parameters"`
	RequestBody requestBody         `json:"requestBody"`
	Responses   map[string]response `json:"responses"`
}

func (bodylessOperation) isOperation() {}
func (bodyOperation) isOperation()     {}

// parameter is one path segment or query parameter a route reads, which every request carries.
type parameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
	Schema   schema `json:"schema"`
}

// requestBody is the JSON body every request to a route carries.
type requestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]mediaType `json:"content"`
}

// response is what a route answers under one status.
type response struct {
	Description string               `json:"description"`
	Content     map[string]mediaType `json:"content"`
}

// mediaType is a body's schema under its media type.
type mediaType struct {
	Schema schema `json:"schema"`
}

// schema is one JSON Schema: a reference, a choice between schemas, or one shape with its type.
//
//sumtype:decl
type schema interface{ isSchema() }

// referenceSchema is the schema the document declares under Ref.
//
//ergo:not-an-error
type referenceSchema struct {
	Ref string `json:"$ref"`
}

// typeSchema is a value of one JSON type, with nothing else said about it.
//
//ergo:not-an-error
type typeSchema struct {
	Type string `json:"type"`
}

// formatSchema is a value of one JSON type written in Format.
//
//ergo:not-an-error
type formatSchema struct {
	Type   string `json:"type"`
	Format string `json:"format"`
}

// integerSchema is a whole number from Minimum to Maximum, both included, written exactly.
//
//ergo:not-an-error
type integerSchema struct {
	Type    string      `json:"type"`
	Minimum json.Number `json:"minimum"`
	Maximum json.Number `json:"maximum"`
}

// formattedIntegerSchema is a whole number from Minimum to Maximum, both included, written exactly,
// whose width is one OpenAPI names as Format.
//
//ergo:not-an-error
type formattedIntegerSchema struct {
	Type    string      `json:"type"`
	Format  string      `json:"format"`
	Minimum json.Number `json:"minimum"`
	Maximum json.Number `json:"maximum"`
}

// enumSchema is a value of one JSON type holding one of Enum.
//
//ergo:not-an-error
type enumSchema struct {
	Type string   `json:"type"`
	Enum []string `json:"enum"`
}

// arraySchema is an array of Items.
//
//ergo:not-an-error
type arraySchema struct {
	Type  string `json:"type"`
	Items schema `json:"items"`
}

// fixedArraySchema is an array of Items holding exactly as many items as MinItems and MaxItems
// both say.
//
//ergo:not-an-error
type fixedArraySchema struct {
	Type     string `json:"type"`
	Items    schema `json:"items"`
	MinItems int64  `json:"minItems"`
	MaxItems int64  `json:"maxItems"`
}

// dictionarySchema is an object whose every value is AdditionalProperties, under any name.
//
//ergo:not-an-error
type dictionarySchema struct {
	Type                 string `json:"type"`
	AdditionalProperties schema `json:"additionalProperties"`
}

// keyedDictionarySchema is an object whose every name is one of PropertyNames and whose every value
// is AdditionalProperties.
//
//ergo:not-an-error
type keyedDictionarySchema struct {
	Type                 string        `json:"type"`
	PropertyNames        propertyNames `json:"propertyNames"`
	AdditionalProperties schema        `json:"additionalProperties"`
}

// propertyNames is the names an object may hold: each one of Enum.
type propertyNames struct {
	Enum []string `json:"enum"`
}

// objectSchema is an object holding exactly Properties, each of them Required.
//
//ergo:not-an-error
type objectSchema struct {
	Type                 string            `json:"type"`
	Properties           map[string]schema `json:"properties"`
	Required             []string          `json:"required"`
	AdditionalProperties bool              `json:"additionalProperties"`
}

// anyOfSchema is a value any of AnyOf accepts.
//
//ergo:not-an-error
type anyOfSchema struct {
	AnyOf []schema `json:"anyOf"`
}

// oneOfSchema is an answer exactly one of OneOf accepts, told apart by the member its tag names.
//
//ergo:not-an-error
type oneOfSchema struct {
	OneOf         []schema      `json:"oneOf"`
	Discriminator discriminator `json:"discriminator"`
}

// discriminator is the field naming which schema an answer is, and the schema each name selects.
type discriminator struct {
	PropertyName string            `json:"propertyName"`
	Mapping      map[string]string `json:"mapping"`
}

// neverSchema accepts no value: it is the schema Not says every value matches.
//
//ergo:not-an-error
type neverSchema struct {
	Not struct{} `json:"not"`
}

func (referenceSchema) isSchema()        {}
func (typeSchema) isSchema()             {}
func (formatSchema) isSchema()           {}
func (integerSchema) isSchema()          {}
func (formattedIntegerSchema) isSchema() {}
func (enumSchema) isSchema()             {}
func (arraySchema) isSchema()            {}
func (fixedArraySchema) isSchema()       {}
func (dictionarySchema) isSchema()       {}
func (keyedDictionarySchema) isSchema()  {}
func (objectSchema) isSchema()           {}
func (anyOfSchema) isSchema()            {}
func (oneOfSchema) isSchema()            {}
func (neverSchema) isSchema()            {}
