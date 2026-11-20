package odin

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

type bindingWriter struct {
	request  native.Request
	graph    compiler.Graph
	imports  []string
	from     []int
	to       []int
	source   *strings.Builder
	problems []error
}

func generateBindings(request native.Request, graph compiler.Graph, routes []routeBinding, _ assembling.APIAssembled) outcome.Result[[]native.File] {
	writer := &bindingWriter{request: request, graph: graph, imports: []string{}, from: []int{}, to: []int{}, source: &strings.Builder{}, problems: []error{}}
	for _, route := range routes {
		writer.collect(route.input, &writer.from)
		for _, variant := range route.output.Variants {
			writer.collect(variant, &writer.to)
		}
		outcome.ForEachOption(route.guard, func(guard guardBinding) {
			writer.importDirectory(filepath.Dir(guard.declaration.File))
			for _, variant := range guard.output.Variants {
				outcome.FoldOption(findAdmitted(graph, variant), func(compiler.Named) struct{} { return struct{}{} }, func() struct{} { writer.collect(variant, &writer.to); return struct{}{} })
			}
		})
	}
	slices.Sort(writer.from)
	slices.Sort(writer.to)
	for _, id := range slices.Concat(writer.from, writer.to) {
		writer.importNative(id)
	}
	for _, route := range routes {
		writer.importDirectory(filepath.Dir(route.declaration.File))
		outcome.ForEachOption(route.guard, func(guard guardBinding) {
			for _, variant := range guard.output.Variants {
				writer.importNative(variant)
			}
		})
	}
	writer.source.WriteString(description.GeneratedHeader + "\npackage nocontract_server\n\n")
	writer.source.WriteString("import \"core:bufio\"\nimport \"core:encoding/json\"\nimport \"core:fmt\"\nimport \"core:io\"\nimport \"core:mem\"\nimport \"core:os\"\n")
	for index, directory := range writer.imports {
		relative := outcome.Fold(outcome.TupleToResult(filepath.Rel(request.Server, directory)), func(path string) string { return path }, func(cause error) string {
			writer.problems = append(writer.problems, compiler.ImportUnresolved{From: request.Server, Package: directory, Cause: cause})
			return ""
		})
		fmt.Fprintf(writer.source, "import nc_package_%d %s\n", index, strconv.Quote(filepath.ToSlash(relative)))
	}
	writer.source.WriteString(bindingRuntime)
	wires := slices.Concat(writer.from, writer.to)
	slices.Sort(wires)
	wires = slices.Compact(wires)
	for _, id := range wires {
		writer.wireDeclaration(id)
	}
	for _, id := range writer.from {
		writer.fromDeclaration(id)
	}
	for _, id := range writer.to {
		writer.toDeclaration(id)
	}
	for position, route := range routes {
		writer.routeDeclaration(position, route)
		outcome.ForEachOption(route.guard, func(guard guardBinding) { writer.guardDeclaration(position, guard) })
	}
	writer.source.WriteString("\n@(private, require_results)\ndispatch :: proc(line: []u8) -> Reply {\n operation := struct #all_or_none { operation: string }{operation = \"\"}\n switch failure in json.unmarshal(line, &operation, spec = .JSON) {\n case nil:\n case json.Error, json.Unmarshal_Data_Error, json.Unsupported_Type_Error: return Wire_Refused{reason = fmt.tprintf(\"decoding operation: %v\", failure)}\n }\n switch operation.operation {\n")
	for position, route := range routes {
		fmt.Fprintf(writer.source, " case %s: return call_route_%d(line)\n", strconv.Quote(description.OperationID(route.declaration.Entity.Package, route.endpoint)), position)
		outcome.ForEachOption(route.guard, func(guard guardBinding) {
			fmt.Fprintf(writer.source, " case %s: return call_guard_%d(line)\n", strconv.Quote(native.GuardOperationID(route.declaration.Entity.Package, route.endpoint)), position)
		})
	}
	writer.source.WriteString(" }\n return Wire_Refused{reason = fmt.tprintf(\"unknown native operation %q\", operation.operation)}\n}\n")
	return outcome.FoldOption(outcome.EmptyableToOption(errors.Join(writer.problems...)), outcome.Err[[]native.File], func() outcome.Result[[]native.File] {
		return outcome.Ok([]native.File{{Path: filepath.Join(request.Server, "nocontract_bindings.odin"), Contents: []byte(writer.source.String())}})
	})
}

func (writer *bindingWriter) base(id int) compiler.Type {
	return compiler.Base(writer.graph, id)
}

func (writer *bindingWriter) collect(id int, ids *[]int) {
	if slices.Contains(*ids, id) {
		return
	}
	*ids = append(*ids, id)
	switch checked := writer.base(id).(type) {
	case compiler.Struct:
		for _, field := range checked.Fields {
			writer.collect(field.Type, ids)
		}
	case compiler.Union:
		for _, variant := range checked.Variants {
			writer.collect(variant, ids)
		}
	case compiler.Sequence:
		writer.collect(checked.Element, ids)
	case compiler.Map:
		writer.collect(checked.Key, ids)
		writer.collect(checked.Element, ids)
	case compiler.String, compiler.Integer, compiler.Float, compiler.Boolean, compiler.Enum:
	case compiler.Named, compiler.Procedure, compiler.Unsupported, nil:
		writer.unsupported(id, "wire traversal")
	}
}

func (writer *bindingWriter) importDirectory(directory string) int {
	if found := slices.Index(writer.imports, directory); found >= 0 {
		return found
	}
	writer.imports = append(writer.imports, directory)
	return len(writer.imports) - 1
}

func (writer *bindingWriter) importNative(id int) {
	outcome.ForEachOption(compiler.Select[compiler.Named](writer.graph.Types[id]), func(checked compiler.Named) {
		if checked.Package != "runtime" {
			writer.importDirectory(filepath.Dir(checked.File))
		}
	})
}

func (writer *bindingWriter) typeName(id int) string {
	return outcome.FoldOption(compiler.Select[compiler.Named](writer.graph.Types[id]), func(named compiler.Named) string { return named.Name }, func() string { return writer.unsupported(id, "named outcome") })
}

func (writer *bindingWriter) unsupported(id int, operation string) string {
	writer.problems = append(writer.problems, DeclarationRefused{Position: writer.request.API.Position, Declaration: fmt.Sprintf("%T", writer.graph.Types[id]), Rule: "unsupported compiler type while generating " + operation})
	return ""
}

func (writer *bindingWriter) nativeType(id int) string {
	return outcome.FoldOption(compiler.Select[compiler.Named](writer.graph.Types[id]), func(checked compiler.Named) string {
		if checked.Package != "runtime" {
			return fmt.Sprintf("nc_package_%d.%s", writer.importDirectory(filepath.Dir(checked.File)), checked.Name)
		}
		return writer.nativeShape(id, writer.base(id))
	}, func() string { return writer.nativeShape(id, writer.base(id)) })
}

func (writer *bindingWriter) nativeShape(id int, shape compiler.Type) string {
	switch checked := shape.(type) {
	case compiler.String:
		return "string"
	case compiler.Boolean:
		return "bool"
	case compiler.Integer:
		prefix := "u"
		if checked.Signed {
			prefix = "i"
		}
		return fmt.Sprintf("%s%d", prefix, checked.Size*8)
	case compiler.Float:
		return fmt.Sprintf("f%d", checked.Size*8)
	case compiler.Sequence:
		switch storage := checked.Storage.(type) {
		case compiler.Slice:
			return "[]" + writer.nativeType(checked.Element)
		case compiler.Dynamic:
			return "[dynamic]" + writer.nativeType(checked.Element)
		case compiler.Fixed:
			return fmt.Sprintf("[%d]%s", storage.Length, writer.nativeType(checked.Element))
		case nil:
			return writer.unsupported(id, "native sequence")
		}
	case compiler.Map:
		return "map[" + writer.nativeType(checked.Key) + "]" + writer.nativeType(checked.Element)
	case compiler.Union:
		return "Maybe(" + writer.nativeType(checked.Variants[0]) + ")"
	case compiler.Struct:
		parts := []string{}
		for _, field := range checked.Fields {
			parts = append(parts, field.Name+": "+writer.nativeType(field.Type))
		}
		return "struct #all_or_none {" + strings.Join(parts, ", ") + "}"
	case compiler.Enum:
		parts := []string{}
		for _, entry := range checked.Cases {
			value := strconv.FormatUint(entry.Value, 10)
			outcome.ForEachOption(compiler.Select[compiler.Integer](writer.base(checked.Base)), func(integer compiler.Integer) {
				if integer.Signed {
					value = strconv.FormatInt(int64(entry.Value), 10)
				}
			})
			parts = append(parts, entry.Name+" = "+value)
		}
		return "enum " + writer.nativeType(checked.Base) + " {" + strings.Join(parts, ", ") + "}"
	case compiler.Named, compiler.Procedure, compiler.Unsupported, nil:
	}
	return writer.unsupported(id, "native type")
}

func (writer *bindingWriter) wireType(id int) string {
	checked := writer.base(id)
	switch checked := checked.(type) {
	case compiler.Struct:
		return fmt.Sprintf("Wire_%d", id)
	case compiler.Enum, compiler.String:
		return "string"
	case compiler.Boolean, compiler.Integer, compiler.Float:
		// Primitive aliases are deliberately omitted: the codec takes the native primitive.
		return writer.nativeShape(id, checked)
	case compiler.Sequence:
		switch storage := checked.Storage.(type) {
		case compiler.Slice, compiler.Dynamic:
			return "[]" + writer.wireType(checked.Element)
		case compiler.Fixed:
			return fmt.Sprintf("[%d]%s", storage.Length, writer.wireType(checked.Element))
		case nil:
			return writer.unsupported(id, "wire sequence")
		}
	case compiler.Map:
		return "map[string]" + writer.wireType(checked.Element)
	case compiler.Union:
		return "Maybe(" + writer.wireType(checked.Variants[0]) + ")"
	case compiler.Named, compiler.Procedure, compiler.Unsupported, nil:
	}
	return writer.unsupported(id, "wire type")
}

func (writer *bindingWriter) wireZero(id int) string {
	checked := writer.base(id)
	switch checked := checked.(type) {
	case compiler.String, compiler.Enum:
		return "\"\""
	case compiler.Boolean:
		return "false"
	case compiler.Integer, compiler.Float:
		return "0"
	case compiler.Struct:
		parts := []string{}
		for _, field := range checked.Fields {
			parts = append(parts, field.Name+" = "+writer.wireZero(field.Type))
		}
		return writer.wireType(id) + "{" + strings.Join(parts, ", ") + "}"
	case compiler.Sequence, compiler.Map, compiler.Union:
		return writer.wireType(id) + "{}"
	case compiler.Named, compiler.Procedure, compiler.Unsupported, nil:
	}
	return writer.unsupported(id, "wire initialization")
}

func (writer *bindingWriter) wireDeclaration(id int) {
	outcome.ForEachOption(compiler.Select[compiler.Struct](writer.base(id)), func(checked compiler.Struct) {
		fmt.Fprintf(writer.source, "\n@(private)\nWire_%d :: struct #all_or_none {\n", id)
		for _, field := range checked.Fields {
			fmt.Fprintf(writer.source, " %s: %s", field.Name, writer.wireType(field.Type))
			if field.Tag != "" {
				fmt.Fprintf(writer.source, " `%s`", field.Tag)
			}
			writer.source.WriteString(",\n")
		}
		writer.source.WriteString("}\n")
	})
}

func (writer *bindingWriter) fromDeclaration(id int) {
	nativeType := writer.nativeType(id)
	fmt.Fprintf(writer.source, "\n@(private)\nDecoded_%d :: struct #all_or_none { value: %s }\n@(private)\nDecode_%d :: union #no_nil { Decoded_%d, Wire_Refused }\n", id, nativeType, id, id)
	fmt.Fprintf(writer.source, "@(private, require_results)\nfrom_wire_%d :: proc(input: %s) -> Decode_%d {\n", id, writer.wireType(id), id)
	checked := writer.base(id)
	switch checked := checked.(type) {
	case compiler.String, compiler.Integer, compiler.Float, compiler.Boolean:
		conversion := "input"
		outcome.ForEachOption(compiler.Select[compiler.Named](writer.graph.Types[id]), func(compiler.Named) { conversion = nativeType + "(input)" })
		fmt.Fprintf(writer.source, " return Decoded_%d{value = %s}\n", id, conversion)
	case compiler.Enum:
		writer.source.WriteString(" switch input {\n")
		enumType := nativeType
		outcome.FoldOption(compiler.Select[compiler.Named](writer.graph.Types[id]), func(compiler.Named) struct{} { return struct{}{} }, func() struct{} { enumType = "(" + nativeType + ")"; return struct{}{} })
		for _, name := range compiler.EnumNames(checked) {
			fmt.Fprintf(writer.source, " case %s: return Decoded_%d{value = %s.%s}\n", strconv.Quote(name), id, enumType, name)
		}
		writer.source.WriteString(" }\n")
		fmt.Fprintf(writer.source, " return Wire_Refused{reason = fmt.tprintf(%s, input)}\n", strconv.Quote("decoding "+nativeType+": unknown enum case %q"))
	case compiler.Struct:
		writer.fromStructFields(id, checked.Fields, 0, []string{})
	case compiler.Union:
		child := checked.Variants[0]
		fmt.Fprintf(writer.source, " switch held in input {\n case %s:\n switch decoded in from_wire_%d(held) {\n case Decoded_%d: return Decoded_%d{value = decoded.value}\n case Wire_Refused: return decoded\n }\n case nil: return Decoded_%d{value = {}}\n }\n unreachable()\n", writer.wireType(child), child, child, id, id)
	case compiler.Sequence:
		container, assignment := "", ""
		switch checked.Storage.(type) {
		case compiler.Slice:
			container = "make(" + nativeType + ", len(input))"
			assignment = "output[index] = decoded.value"
		case compiler.Dynamic:
			container = "make(" + nativeType + ")"
			assignment = "append(&output, decoded.value)\n _ = index"
		case compiler.Fixed:
			container = nativeType + "{}"
			assignment = "output[index] = decoded.value"
		case nil:
			writer.unsupported(id, "native sequence construction")
		}
		fmt.Fprintf(writer.source, " output := %s\n for value, index in input {\n switch decoded in from_wire_%d(value) {\n case Decoded_%d:\n", container, checked.Element, checked.Element)
		writer.source.WriteString(" " + assignment + "\n")
		fmt.Fprintf(writer.source, " case Wire_Refused: return decoded\n }\n }\n return Decoded_%d{value = output}\n", id)
	case compiler.Map:
		fmt.Fprintf(writer.source, " output := make(%s)\n for key, value in input {\n switch decoded_key in from_wire_%d(key) {\n case Decoded_%d:\n switch decoded_value in from_wire_%d(value) {\n case Decoded_%d: output[decoded_key.value] = decoded_value.value\n case Wire_Refused: return decoded_value\n }\n case Wire_Refused: return decoded_key\n }\n }\n return Decoded_%d{value = output}\n", nativeType, checked.Key, checked.Key, checked.Element, checked.Element, id)
	case compiler.Named, compiler.Procedure, compiler.Unsupported, nil:
		writer.unsupported(id, "native decoding")
	}
	writer.source.WriteString("}\n")
}

func (writer *bindingWriter) fromStructFields(id int, fields []compiler.Field, position int, built []string) {
	if position == len(fields) {
		fmt.Fprintf(writer.source, " return Decoded_%d{value = %s{%s}}\n", id, writer.nativeType(id), strings.Join(built, ", "))
		return
	}
	field := fields[position]
	fmt.Fprintf(writer.source, " switch field_%d in from_wire_%d(input.%s) {\n case Decoded_%d:\n", position, field.Type, field.Name, field.Type)
	writer.fromStructFields(id, fields, position+1, append(slices.Clone(built), fmt.Sprintf("%s = field_%d.value", field.Name, position)))
	fmt.Fprintf(writer.source, " case Wire_Refused: return field_%d\n }\n unreachable()\n", position)
}

func (writer *bindingWriter) toDeclaration(id int) {
	checked := writer.base(id)
	fmt.Fprintf(writer.source, "\n@(private, require_results)\nto_wire_%d :: proc(input: %s) -> %s {\n", id, writer.nativeType(id), writer.wireType(id))
	switch checked := checked.(type) {
	case compiler.String, compiler.Integer, compiler.Float, compiler.Boolean:
		conversion := "input"
		outcome.ForEachOption(compiler.Select[compiler.Named](writer.graph.Types[id]), func(compiler.Named) { conversion = writer.wireType(id) + "(input)" })
		fmt.Fprintf(writer.source, " return %s\n", conversion)
	case compiler.Enum:
		writer.source.WriteString(" switch input {\n")
		for _, name := range compiler.EnumNames(checked) {
			fmt.Fprintf(writer.source, " case .%s: return %s\n", name, strconv.Quote(name))
		}
		writer.source.WriteString(" }\n unreachable()\n")
	case compiler.Struct:
		parts := []string{}
		for _, field := range checked.Fields {
			parts = append(parts, fmt.Sprintf("%s = to_wire_%d(input.%s)", field.Name, field.Type, field.Name))
		}
		fmt.Fprintf(writer.source, " return %s{%s}\n", writer.wireType(id), strings.Join(parts, ", "))
	case compiler.Union:
		child := checked.Variants[0]
		fmt.Fprintf(writer.source, " switch held in input {\n case %s: return to_wire_%d(held)\n case nil: return {}\n }\n unreachable()\n", writer.nativeType(child), child)
	case compiler.Sequence:
		container := "make(" + writer.wireType(id) + ", len(input))"
		switch checked.Storage.(type) {
		case compiler.Fixed:
			container = writer.wireType(id) + "{}"
		case compiler.Slice, compiler.Dynamic:
		case nil:
			writer.unsupported(id, "wire sequence construction")
		}
		fmt.Fprintf(writer.source, " output := %s\n for value, index in input { output[index] = to_wire_%d(value) }\n return output\n", container, checked.Element)
	case compiler.Map:
		fmt.Fprintf(writer.source, " output := make(%s)\n for key, value in input { output[to_wire_%d(key)] = to_wire_%d(value) }\n return output\n", writer.wireType(id), checked.Key, checked.Element)
	case compiler.Named, compiler.Procedure, compiler.Unsupported, nil:
		writer.unsupported(id, "native encoding")
	}
	writer.source.WriteString("}\n")
}

func (writer *bindingWriter) routeDeclaration(position int, route routeBinding) {
	site := writer.projectSite(route.declaration) + " route " + route.declaration.Entity.Package + "." + route.declaration.Entity.Name
	fmt.Fprintf(writer.source, "\n@(private, require_results)\ncall_route_%d :: proc(line: []u8) -> Reply {\n request := struct #all_or_none { operation: string, input: %s }{operation = \"\", input = %s}\n", position, writer.wireType(route.input), writer.wireZero(route.input))
	writer.decodeRequest(site)
	fmt.Fprintf(writer.source, " switch input in from_wire_%d(request.input) {\n case Decoded_%d:\n switch answer in nc_package_%d.%s(input.value) {\n", route.input, route.input, writer.importDirectory(filepath.Dir(route.declaration.File)), route.declaration.Entity.Name)
	writer.unionBranches(route.output, site, writer.answerMember)
	fmt.Fprintf(writer.source, " }\n case Wire_Refused: return Wire_Refused{reason = fmt.tprintf(\"%%s: %%s\", %s, input.reason)}\n }\n unreachable()\n}\n", strconv.Quote(site))
}

func (writer *bindingWriter) guardDeclaration(position int, guard guardBinding) {
	site := writer.projectSite(guard.declaration) + " guard " + guard.declaration.Entity.Package + "." + guard.declaration.Entity.Name
	fmt.Fprintf(writer.source, "\n@(private, require_results)\ncall_guard_%d :: proc(line: []u8) -> Reply {\n request := struct #all_or_none { operation: string, input: struct #all_or_none { scope: string, headers: map[string]string } }{operation = \"\", input = {scope = \"\", headers = {}}}\n", position)
	writer.decodeRequest(site)
	fmt.Fprintf(writer.source, " switch answer in nc_package_%d.%s(request.input.scope, request.input.headers) {\n", writer.importDirectory(filepath.Dir(guard.declaration.File)), guard.declaration.Entity.Name)
	writer.unionBranches(guard.output, site, func(variant int) {
		outcome.FoldOption(findAdmitted(writer.graph, variant), func(compiler.Named) struct{} {
			writer.source.WriteString(" return json_reply(struct #all_or_none {admitted: struct #all_or_none {}}{admitted = {}})\n")
			return struct{}{}
		}, func() struct{} { writer.answerMember(variant); return struct{}{} })
	})
	writer.source.WriteString(" }\n unreachable()\n}\n")
}

func (writer *bindingWriter) decodeRequest(operation string) {
	fmt.Fprintf(writer.source, " switch failure in json.unmarshal(line, &request, spec = .JSON) {\n case nil:\n case json.Error, json.Unmarshal_Data_Error, json.Unsupported_Type_Error: return Wire_Refused{reason = fmt.tprintf(\"decoding %%s: %%v\", %s, failure)}\n }\n", strconv.Quote(operation))
}

func (writer *bindingWriter) unionBranches(checked compiler.Union, site string, emit func(int)) {
	for _, variant := range checked.Variants {
		fmt.Fprintf(writer.source, " case %s:\n", writer.nativeType(variant))
		emit(variant)
	}
	if !checked.NoNil {
		fmt.Fprintf(writer.source, " case nil: return Wire_Refused{reason = %s}\n", strconv.Quote(site+" returned a nil outcome"))
	}
}

func (writer *bindingWriter) projectSite(declaration compiler.Declaration) string {
	position := placed(declaration)
	return outcome.Fold(outcome.TupleToResult(filepath.Rel(writer.request.Directory, position.File)), func(relative string) string {
		return (assembling.Position{File: filepath.ToSlash(relative), Line: position.Line, Column: position.Column}).String()
	}, func(cause error) string {
		writer.problems = append(writer.problems, portableSourceSiteUnavailable{Project: writer.request.Directory, Source: position, Cause: cause})
		return ""
	})
}

func (writer *bindingWriter) answerMember(variant int) {
	fmt.Fprintf(writer.source, " return json_reply(struct #all_or_none {answered: struct #all_or_none {tag: string, fields: %s}}{answered = {tag = %s, fields = to_wire_%d(answer)}})\n", writer.wireType(variant), strconv.Quote(writer.typeName(variant)), variant)
}
