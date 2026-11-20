package compiler

import "github.com/CheckPickerUpper/outcome"

// Type is one compiler-resolved native type.
//
//sumtype:decl
type Type interface{ isType() }

// Named is a type declared by a source package.
//
//ergo:not-an-error
type Named struct {
	Name    string
	Package string
	File    string
	Line    int
	Column  int
	Base    int
}

// Integer is a native integer with the compiler's signedness and byte width.
//
//ergo:not-an-error
type Integer struct {
	Size   int
	Signed bool
}

// Float is a native floating-point value of Size bytes.
//
//ergo:not-an-error
type Float struct{ Size int }

// String is a native UTF-8 string.
//
//ergo:not-an-error
type String struct{}

// Boolean is a native Boolean.
//
//ergo:not-an-error
type Boolean struct{}

// Struct is a native struct's fields in declaration order.
//
//ergo:not-an-error
type Struct struct{ Fields []Field }

// Union is a native union's alternatives and whether nil is a member.
//
//ergo:not-an-error
type Union struct {
	Variants []int
	NoNil    bool
}

// Enum is a native enumeration's exact case names.
//
//ergo:not-an-error
type Enum struct {
	Cases []EnumCase
	Base  int
}

// EnumCase is one compiler-resolved enum name and its integer value.
type EnumCase struct {
	Name  string
	Value uint64
}

// EnumNames is each case's exact native spelling, in declaration order.
func EnumNames(enumeration Enum) []string {
	names := []string{}
	for _, entry := range enumeration.Cases {
		names = append(names, entry.Name)
	}
	return names
}

// Sequence is a native sequence with its storage and element type.
//
//ergo:not-an-error
type Sequence struct {
	Element int
	Storage Storage
}

// Storage is a native sequence's allocation and length contract.
//
//sumtype:decl
type Storage interface{ isStorage() }

// Fixed is an array of exactly Length elements.
//
//ergo:not-an-error
type Fixed struct{ Length int }

// Slice is a native slice.
//
//ergo:not-an-error
type Slice struct{}

// Dynamic is a native dynamic array.
//
//ergo:not-an-error
type Dynamic struct{}

// Map is a native map with its checked key and value types.
//
//ergo:not-an-error
type Map struct {
	Key     int
	Element int
}

// Procedure is a compiler-resolved procedure signature.
//
//ergo:not-an-error
type Procedure struct {
	Parameters []int
	Results    []int
}

// Unsupported is a compiler type whose runtime shape has no JSON representation.
//
//ergo:not-an-error
type Unsupported struct {
	Kind string
	Name string
}

func (Named) isType()       {}
func (Integer) isType()     {}
func (Float) isType()       {}
func (String) isType()      {}
func (Boolean) isType()     {}
func (Struct) isType()      {}
func (Union) isType()       {}
func (Enum) isType()        {}
func (Sequence) isType()    {}
func (Fixed) isStorage()    {}
func (Slice) isStorage()    {}
func (Dynamic) isStorage()  {}
func (Map) isType()         {}
func (Procedure) isType()   {}
func (Unsupported) isType() {}

func resolvedType(raw compilerTypeJSON) Type {
	switch raw.Kind {
	case "Named":
		return Named{Name: raw.Name, Package: raw.Package, File: raw.File, Line: raw.Line, Column: raw.Column, Base: raw.Base}
	case "Integer":
		return Integer{Size: raw.Size, Signed: raw.Signed}
	case "Float":
		return Float{Size: raw.Size}
	case "String":
		return String{}
	case "Boolean":
		return Boolean{}
	case "Struct":
		return Struct{Fields: raw.Fields}
	case "Union":
		return Union{Variants: raw.Variants, NoNil: raw.NoNil}
	case "Enum":
		cases := []EnumCase{}
		for position, name := range raw.Names {
			cases = append(cases, EnumCase{Name: name, Value: raw.Values[position]})
		}
		return Enum{Cases: cases, Base: raw.Base}
	case "Array":
		return Sequence{Element: raw.Element, Storage: Fixed{Length: raw.Length}}
	case "List":
		return Sequence{Element: raw.Element, Storage: Slice{}}
	case "Dynamic":
		return Sequence{Element: raw.Element, Storage: Dynamic{}}
	case "Map":
		return Map{Key: raw.Key, Element: raw.Element}
	case "Procedure":
		return Procedure{Parameters: raw.Parameters, Results: raw.Results}
	}
	return Unsupported{Kind: raw.Kind, Name: raw.Name}
}

// Base follows named declarations to their compiler-resolved shape.
func Base(graph Graph, id int) Type {
	checked := graph.Types[id]
	for {
		switch typed := checked.(type) {
		case Named:
			checked = graph.Types[typed.Base]
		case Integer, Float, String, Boolean, Struct, Union, Enum, Sequence, Map, Procedure, Unsupported, nil:
			return checked
		}
	}
}

// Select narrows one compiler type to the shape the caller can consume.
func Select[T Type](checked Type) outcome.Option[T] {
	typed, present := checked.(T)
	return outcome.TupleToOption(typed, present)
}
