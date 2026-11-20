package projectfile

import "github.com/CheckPickerUpper/nocontract/assembling"

// ProjectNotJSON is a project file that is not one JSON value, and why, at the place reading stopped.
//
//ergo:code NC-1082
//ergo:message The project file is not JSON: {Reason}.
type ProjectNotJSON struct{ Reason string }

// ProjectNotObject is a project file holding a JSON value that is not an object, as it is written.
//
//ergo:code NC-1083
//ergo:message The project file holds {Written}, where it must hold an object.
type ProjectNotObject struct{ Written string }

// KeyMissing is an object of the project file without a key it must have, at the object.
//
//ergo:code NC-1084
//ergo:message The project file has no {Key}.
type KeyMissing struct{ Key string }

// ValueWrongType is a key whose value is not the JSON type the key holds, as it is written.
//
//ergo:code NC-1085
//ergo:message {Key} is {Written}, where it must be {Want, select, JSONText {a string} JSONObject {an object} JSONArray {an array}}.
type ValueWrongType struct {
	Key     string
	Written string
	Want    JSONType
}

// ValueEmpty is a key whose value is an empty string or array, as it is written.
//
//ergo:code NC-1086
//ergo:message {Key} is {Written}, which names nothing.
type ValueEmpty struct {
	Key     string
	Written string
}

// KeyUnknown is a key no project file holds.
//
//ergo:code NC-1087
//ergo:message {Key} is not a key a project file holds.
type KeyUnknown struct{ Key string }

// KeyRepeated is a key written again in one object, and where it is first written.
//
//ergo:code NC-1088
//ergo:message {Key} is written more than once.
type KeyRepeated struct {
	Key   string
	First assembling.Position
}

// LanguageUnknown is a project file naming a language NoContract reads no backend in.
//
//ergo:code NC-1055
//ergo:message The project file names the language "{Language}". NoContract reads {Supported}.
type LanguageUnknown struct {
	Language  string
	Supported string
}

// JSONType is the JSON type a key's value must have.
//
//sumtype:decl
type JSONType interface{ isJSONType() }

// JSONText is a JSON string.
//
//ergo:not-an-error
type JSONText struct{}

// JSONObject is a JSON object.
//
//ergo:not-an-error
type JSONObject struct{}

// JSONArray is a JSON array.
//
//ergo:not-an-error
type JSONArray struct{}

func (JSONText) isJSONType()   {}
func (JSONObject) isJSONType() {}
func (JSONArray) isJSONType()  {}
