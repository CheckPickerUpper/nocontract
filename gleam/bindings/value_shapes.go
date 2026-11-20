// Package bindings generates typed Gleam codecs and exhaustive native dispatch.
package bindings

import (
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
)

// Value is a checked native value and the JSON shape derived from it.
//
//sumtype:decl
type Value interface {
	isValue()
	Schema() description.Schema
}

// Scalar is a value represented by a JSON scalar inside a named scalar wrapper.
type Scalar interface {
	Value
	isScalar()
}

// DictionaryKey is a string or enumeration that can name a JSON property.
type DictionaryKey interface {
	Value
	WireKey() description.DictionaryKey
}

// Text is Gleam String represented by a JSON string.
//
//ergo:not-an-error
type Text struct{}

// Whole is Gleam Int represented within the declared wire width.
//
//ergo:not-an-error
type Whole struct{ Width description.IntegerWidth }

// Number is Gleam Float represented by a finite JSON number.
//
//ergo:not-an-error
type Number struct{}

// Truth is Gleam Bool represented by a JSON boolean.
//
//ergo:not-an-error
type Truth struct{}

// Timestamp is an RFC 3339 string inside an explicit Instant wrapper.
//
//ergo:not-an-error
type Timestamp struct{}

// Record is one named single-constructor record.
//
//ergo:not-an-error
type Record struct {
	Native      compiler.Named
	Constructor compiler.Constructor
	Fields      []Field
}

// RecordRef points to a checked record by its compiler identity.
//
//ergo:not-an-error
type RecordRef struct{ Native compiler.Named }

// Literal is one nullary constructor and its individual wire spelling.
type Literal struct {
	Constructor string
	Text        string
}

// Enumeration is a custom type with only nullary constructors.
//
//ergo:not-an-error
type Enumeration struct {
	Native   compiler.Named
	Literals []Literal
}

// List is a Gleam list.
//
//ergo:not-an-error
type List struct {
	Native compiler.Named
	Item   Value
}

// Fixed is a homogeneous tuple represented as a fixed-length array.
//
//ergo:not-an-error
type Fixed struct {
	Native compiler.Tuple
	Item   Value
}

// Dictionary is a dictionary whose keys are strings or nullary constructors.
//
//ergo:not-an-error
type Dictionary struct {
	Native compiler.Named
	Key    DictionaryKey
	Item   Value
}

// Nullable is a Gleam Option represented by JSON null or its contained value.
//
//ergo:not-an-error
type Nullable struct {
	Native compiler.Named
	Inner  Value
}

// Wrapped is a named scalar wrapper with its checked constructor and contained scalar.
//
//ergo:not-an-error
type Wrapped struct {
	Native      compiler.Named
	Constructor compiler.Constructor
	Inner       Scalar
}

// Field is a labelled constructor field and the value represented on the wire.
type Field struct {
	Name  description.FieldName
	Value Value
}

func (Text) isValue()        {}
func (Whole) isValue()       {}
func (Number) isValue()      {}
func (Truth) isValue()       {}
func (Timestamp) isValue()   {}
func (Text) isScalar()       {}
func (Whole) isScalar()      {}
func (Number) isScalar()     {}
func (Truth) isScalar()      {}
func (Timestamp) isScalar()  {}
func (Record) isValue()      {}
func (RecordRef) isValue()   {}
func (Enumeration) isValue() {}
func (List) isValue()        {}
func (Fixed) isValue()       {}
func (Dictionary) isValue()  {}
func (Nullable) isValue()    {}
func (Wrapped) isValue()     {}

// Schema is a JSON string.
func (Text) Schema() description.Schema { return description.Text{} }

// WireKey is any JSON property name.
func (Text) WireKey() description.DictionaryKey { return description.Text{} }

// Schema is a whole number of the declared width.
func (value Whole) Schema() description.Schema { return description.Integer{Width: value.Width} }

// Schema is a finite JSON number.
func (Number) Schema() description.Schema { return description.Finite{} }

// Schema is a JSON boolean.
func (Truth) Schema() description.Schema { return description.Truth{} }

// Schema is an RFC 3339 timestamp string.
func (Timestamp) Schema() description.Schema { return description.Instant{} }

// Schema is a reference to the compiler's named record.
func (value Record) Schema() description.Schema {
	return description.StructRef{Name: value.Native.Identity.Name}
}

// Schema is the record's component reference.
func (value RecordRef) Schema() description.Schema {
	return description.StructRef{Name: value.Native.Identity.Name}
}

// Schema is every permitted constructor spelling.
func (value Enumeration) Schema() description.Schema {
	return value.literals()
}

// WireKey is one of the enumeration's permitted property names.
func (value Enumeration) WireKey() description.DictionaryKey { return value.literals() }

func (value Enumeration) literals() description.Literals {
	values := []string{}
	for _, literal := range value.Literals {
		values = append(values, literal.Text)
	}
	return description.Literals{Values: values}
}

// Schema is an arbitrary-length JSON array.
func (value List) Schema() description.Schema { return description.List{Item: value.Item.Schema()} }

// Schema is an array with the tuple's exact length.
func (value Fixed) Schema() description.Schema {
	return description.FixedList{Item: value.Item.Schema(), Length: int64(len(value.Native.Elements))}
}

// Schema is an object with the checked key and value representations.
func (value Dictionary) Schema() description.Schema {
	return description.Dictionary{Key: value.Key.WireKey(), Value: value.Item.Schema()}
}

// Schema is the contained shape or null.
func (value Nullable) Schema() description.Schema {
	return description.Nullable{Inner: value.Inner.Schema()}
}

// Schema is the scalar contained by the named wrapper.
func (value Wrapped) Schema() description.Schema { return value.Inner.Schema() }

// Fields describes the fields derived from labelled constructor parameters.
func Fields(fields []Field) []description.Field {
	described := []description.Field{}
	for _, field := range fields {
		described = append(described, description.Field{Name: field.Name, Schema: field.Value.Schema()})
	}
	return described
}

// Answer is one exhaustive outcome branch, directly answered or transparently carrying an answer.
//
//sumtype:decl
type Answer interface {
	isAnswer()
	Members() []description.Member
}

// Direct is a marked answer constructor and its checked status and fields.
//
//ergo:not-an-error
type Direct struct {
	Constructor compiler.Constructor
	Answer      description.Answer
	Fields      []Field
}

// Forwarded is an unmarked positional constructor containing a checked named outcome.
//
//ergo:not-an-error
type Forwarded struct {
	Constructor compiler.Constructor
	Inner       Outcome
}

func (Direct) isAnswer()    {}
func (Forwarded) isAnswer() {}

// Members is the single answer this constructor declares.
func (answer Direct) Members() []description.Member {
	return []description.Member{{Name: answer.Constructor.Name, Answer: answer.Answer, Fields: Fields(answer.Fields)}}
}

// Members is the checked answers contained by this positional constructor.
func (answer Forwarded) Members() []description.Member { return answer.Inner.Members() }

// Outcome is a named custom return type with every compiler-exported constructor accounted for.
type Outcome struct {
	Native  compiler.Named
	Answers []Answer
}

// Members returns every checked wire answer in compiler constructor order.
func (value Outcome) Members() []description.Member {
	members := []description.Member{}
	for _, answer := range value.Answers {
		members = append(members, answer.Members()...)
	}
	return members
}

// Route calls one public native function with a checked record input.
type Route struct {
	Module   string
	Function string
	Group    string
	Endpoint string
	Input    Record
	Output   Outcome
}

// Guard calls one public native guard whose Option contains a checked refusal type.
type Guard struct {
	Module    string
	Function  string
	Operation string
	Output    Outcome
}
