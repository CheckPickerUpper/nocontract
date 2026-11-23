package compiler

const reflectionSource = `package nocontract_probe

import "base:runtime"
import "core:encoding/json"
import "core:fmt"
import "core:os"

Probe_Kind :: enum { Named, Integer, Float, String, Boolean, Struct, Union, Enum, Array, List, Dynamic, Map, Procedure, Unsupported }

Field :: struct #all_or_none {
	name: string,
	type: int,
	tag: string,
	embedded: bool ` + "`json:\"using\"`" + `,
}

Probe_Type :: struct #all_or_none {
	kind: Probe_Kind,
	name: string,
	package_name: string ` + "`json:\"package\"`" + `,
	file: string,
	line: i32,
	column: i32,
	size: int,
	base: int,
	element: int,
	key: int,
	length: int,
	signed: bool,
	no_nil: bool,
	fields: [dynamic]Field,
	variants: [dynamic]int,
	names: []string,
	values: [dynamic]u64,
	parameters: [dynamic]int,
	results: [dynamic]int,
}

Root :: struct #all_or_none {
	package_name: string ` + "`json:\"package\"`" + `,
	name: string,
	type: int,
}

Graph :: struct #all_or_none {
	types: [dynamic]Probe_Type,
	roots: [dynamic]Root,
}

@(private, require_results)
remember_type :: proc(checked: ^runtime.Type_Info, known: ^map[typeid]int, queue: ^[dynamic]^runtime.Type_Info) -> int {
	index, present := known^[checked.id]
	if present {
		return index
	}
	index = len(queue)
	known^[checked.id] = index
	append(queue, checked)
	return index
}

@(private, require_results)
describe_type :: proc(checked: ^runtime.Type_Info, known: ^map[typeid]int, queue: ^[dynamic]^runtime.Type_Info) -> Probe_Type {
	resolved := Probe_Type{
		kind = .Unsupported, name = fmt.tprintf("%v",checked.id), package_name = "", file = "", line = 0, column = 0,
		size = checked.size, base = -1, element = -1, key = -1, length = 0, signed = false,
		no_nil = false, fields = {}, variants = {}, names = {}, values = {}, parameters = {}, results = {},
	}
	switch variant in checked.variant {
	case ^runtime.Type_Info_Named:
		resolved.kind = .Named
		resolved.name = variant.name
		resolved.package_name = variant.pkg
		resolved.base = remember_type(variant.base, known, queue)
		resolved.file = variant.loc.file_path
		resolved.line = variant.loc.line
		resolved.column = variant.loc.column
	case ^runtime.Type_Info_Integer:
		resolved.kind = .Integer
		resolved.signed = variant.signed
	case ^runtime.Type_Info_Float:
		resolved.kind = .Float
	case ^runtime.Type_Info_String:
		if !variant.is_cstring && variant.encoding == .UTF_8 {
			resolved.kind = .String
		}
	case ^runtime.Type_Info_Boolean:
		resolved.kind = .Boolean
	case ^runtime.Type_Info_Struct:
		if .raw_union not_in variant.flags && variant.soa_kind == .None {
			resolved.kind = .Struct
			for index in 0..<int(variant.field_count) {
				append(&resolved.fields, Field{name = variant.names[index], type = remember_type(variant.types[index], known, queue), tag = variant.tags[index], embedded = variant.usings[index]})
			}
		}
	case ^runtime.Type_Info_Union:
		resolved.kind = .Union
		resolved.no_nil = variant.no_nil
		for member in variant.variants {
			append(&resolved.variants, remember_type(member, known, queue))
		}
	case ^runtime.Type_Info_Enum:
		resolved.kind = .Enum
		resolved.names = variant.names
		resolved.base = remember_type(variant.base,known,queue)
		for value in variant.values { append(&resolved.values,u64(value)) }
	case ^runtime.Type_Info_Array:
		resolved.kind = .Array
		resolved.element = remember_type(variant.elem, known, queue)
		resolved.length = variant.count
	case ^runtime.Type_Info_Slice:
		resolved.kind = .List
		resolved.element = remember_type(variant.elem, known, queue)
	case ^runtime.Type_Info_Dynamic_Array:
		resolved.kind = .Dynamic
		resolved.element = remember_type(variant.elem, known, queue)
	case ^runtime.Type_Info_Map:
		resolved.kind = .Map
		resolved.key = remember_type(variant.key, known, queue)
		resolved.element = remember_type(variant.value, known, queue)
	case ^runtime.Type_Info_Procedure:
		resolved.kind = .Procedure
		switch parameters in variant.params.variant {
		case ^runtime.Type_Info_Parameters:
			for parameter in parameters.types {
				append(&resolved.parameters, remember_type(parameter, known, queue))
			}
		case nil, ^runtime.Type_Info_Named, ^runtime.Type_Info_Integer, ^runtime.Type_Info_Rune, ^runtime.Type_Info_Float, ^runtime.Type_Info_Complex, ^runtime.Type_Info_Quaternion, ^runtime.Type_Info_String, ^runtime.Type_Info_Boolean, ^runtime.Type_Info_Any, ^runtime.Type_Info_Type_Id, ^runtime.Type_Info_Pointer, ^runtime.Type_Info_Multi_Pointer, ^runtime.Type_Info_Procedure, ^runtime.Type_Info_Array, ^runtime.Type_Info_Enumerated_Array, ^runtime.Type_Info_Dynamic_Array, ^runtime.Type_Info_Slice, ^runtime.Type_Info_Struct, ^runtime.Type_Info_Union, ^runtime.Type_Info_Enum, ^runtime.Type_Info_Map, ^runtime.Type_Info_Bit_Set, ^runtime.Type_Info_Simd_Vector, ^runtime.Type_Info_Matrix, ^runtime.Type_Info_Soa_Pointer, ^runtime.Type_Info_Bit_Field, ^runtime.Type_Info_Fixed_Capacity_Dynamic_Array:
		}
		switch results in variant.results.variant {
		case ^runtime.Type_Info_Parameters:
			for result in results.types {
				append(&resolved.results, remember_type(result, known, queue))
			}
		case nil, ^runtime.Type_Info_Named, ^runtime.Type_Info_Integer, ^runtime.Type_Info_Rune, ^runtime.Type_Info_Float, ^runtime.Type_Info_Complex, ^runtime.Type_Info_Quaternion, ^runtime.Type_Info_String, ^runtime.Type_Info_Boolean, ^runtime.Type_Info_Any, ^runtime.Type_Info_Type_Id, ^runtime.Type_Info_Pointer, ^runtime.Type_Info_Multi_Pointer, ^runtime.Type_Info_Procedure, ^runtime.Type_Info_Array, ^runtime.Type_Info_Enumerated_Array, ^runtime.Type_Info_Dynamic_Array, ^runtime.Type_Info_Slice, ^runtime.Type_Info_Struct, ^runtime.Type_Info_Union, ^runtime.Type_Info_Enum, ^runtime.Type_Info_Map, ^runtime.Type_Info_Bit_Set, ^runtime.Type_Info_Simd_Vector, ^runtime.Type_Info_Matrix, ^runtime.Type_Info_Soa_Pointer, ^runtime.Type_Info_Bit_Field, ^runtime.Type_Info_Fixed_Capacity_Dynamic_Array:
		}
	case nil, ^runtime.Type_Info_Rune, ^runtime.Type_Info_Complex, ^runtime.Type_Info_Quaternion, ^runtime.Type_Info_Any, ^runtime.Type_Info_Type_Id, ^runtime.Type_Info_Pointer, ^runtime.Type_Info_Multi_Pointer, ^runtime.Type_Info_Enumerated_Array, ^runtime.Type_Info_Parameters, ^runtime.Type_Info_Bit_Set, ^runtime.Type_Info_Simd_Vector, ^runtime.Type_Info_Matrix, ^runtime.Type_Info_Soa_Pointer, ^runtime.Type_Info_Bit_Field, ^runtime.Type_Info_Fixed_Capacity_Dynamic_Array:
	}
	return resolved
}

`
