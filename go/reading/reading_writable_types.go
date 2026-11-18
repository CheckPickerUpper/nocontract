package reading

import (
	"go/token"
	"go/types"
	"slices"

	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// FieldUnexportedElsewhere is an unexported field of a struct the route's generated code writes
// out, declared in another package: written there, the struct is another type.
//
//ergo:code NC-1113
//ergo:message The field {Field} is unexported and declared in {Package}, so the route's generated code cannot write the struct that holds it.
type FieldUnexportedElsewhere struct {
	Field   string
	Package string
}

func (FieldUnexportedElsewhere) isProblem() {}

// writableBody is a body value the generated code of the package at from can write: the code
// reading it writes the type of every value it holds, each refused at position when it cannot.
func writableBody(index projectIndex, value serverplan.BodyValue, from string, position token.Pos) reading[serverplan.BodyValue] {
	read := collected(value.Values(), func(held serverplan.BodyValue) reading[types.Type] {
		return writableType(index, held.GoType, from, position)
	})
	return mapped(read, func([]types.Type) serverplan.BodyValue { return value })
}

// writableType is written when the generated code of the package at from can write it: every type
// it names can be named there, and every field of a struct it writes out is exported or declared
// there. A type it cannot name is refused at position, an unexported field where it is declared.
func writableType(index projectIndex, written types.Type, from string, position token.Pos) reading[types.Type] {
	parts := func(inner ...types.Type) reading[types.Type] {
		read := collected(inner, func(part types.Type) reading[types.Type] { return writableType(index, part, from, position) })
		return mapped(read, func([]types.Type) types.Type { return written })
	}
	named := func(object *types.TypeName, arguments *types.TypeList) reading[types.Type] {
		read := paired(reachableName(index, object, from, position), parts(slices.Collect(arguments.Types())...))
		return mapped(read, func(pair[serverplan.GoName, types.Type]) types.Type { return written })
	}
	switch typed := written.(type) {
	case *types.Named:
		return named(typed.Obj(), typed.TypeArgs())
	case *types.Alias:
		return named(typed.Obj(), typed.TypeArgs())
	case *types.Slice:
		return parts(typed.Elem())
	case *types.Array:
		return parts(typed.Elem())
	case *types.Pointer:
		return parts(typed.Elem())
	case *types.Chan:
		return parts(typed.Elem())
	case *types.Map:
		return parts(typed.Key(), typed.Elem())
	case *types.Struct:
		return writableStruct(index, typed, from, position)
	case *types.Basic, *types.Signature, *types.Interface, *types.Tuple, *types.TypeParam, *types.Union, nil:
	}
	return readAs(written, nothingFound())
}

// writableStruct is a struct type the generated code of the package at from writes out field by
// field.
func writableStruct(index projectIndex, written *types.Struct, from string, position token.Pos) reading[types.Type] {
	reads := []reading[types.Type]{}
	for _, field := range indexedFields(written) {
		if owner := packagePath(field.variable); !field.variable.Exported() && owner != from {
			reads = append(reads, notRead[types.Type](index.refused(field.variable.Pos(), FieldUnexportedElsewhere{Field: field.variable.Name(), Package: owner})))
			continue
		}
		reads = append(reads, writableType(index, field.variable.Type(), from, position))
	}
	return mapped(collected(reads, itself), func([]types.Type) types.Type { return written })
}
