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

// endpointBinding pairs the common checked endpoint with the compiler identity of its procedure.
type endpointBinding struct {
	endpoint description.Endpoint
	native   routeBinding
}

type bindingWriter struct {
	request    native.Request
	api        description.API
	graph      compiler.Graph
	imports    []string
	core       []string
	from       []int
	to         []int
	parameters []int
	literals   [][]string
	schemas    map[int]description.Schema
	source     *strings.Builder
	problems   []error
}

func generateBindings(request native.Request, graph compiler.Graph, routes []routeBinding, checked assembling.APIAssembled) outcome.Result[[]native.File] {
	writer := &bindingWriter{request: request, api: checked.API, graph: graph, imports: []string{}, core: []string{}, from: []int{}, to: []int{}, parameters: []int{}, literals: [][]string{}, schemas: map[int]description.Schema{}, source: &strings.Builder{}, problems: []error{}}
	bound := []endpointBinding{}
	for _, group := range checked.API.Groups {
		for _, endpoint := range group.Endpoints {
			position := slices.IndexFunc(routes, func(route routeBinding) bool {
				return route.declaration.Entity.Package == group.Name && route.endpoint == endpoint.Name
			})
			if position < 0 {
				writer.refuse(group.Name+"."+endpoint.Name, "the checked endpoint has no native procedure")
				continue
			}
			binding := endpointBinding{endpoint: endpoint, native: routes[position]}
			bound = append(bound, binding)
			writer.collectEndpoint(binding)
		}
	}
	for _, id := range writer.to {
		writer.wireDeclaration(id)
	}
	for _, id := range writer.from {
		writer.bodyReader(id)
	}
	for _, id := range writer.parameters {
		writer.parameterReader(id)
	}
	for _, id := range writer.to {
		writer.toDeclaration(id)
		writer.answerDeclaration(id)
	}
	writer.sharedResponses()
	writer.source.WriteString(bindingRuntime)
	for position, binding := range bound {
		writer.inputReader(position, binding)
		writer.routeDeclaration(position, binding)
	}
	writer.dispatcher(bound)
	writer.source.WriteString("\n@(private, require_results)\nnc_deadline_exceeded :: proc(route: string, timeout_milliseconds: u32) -> nocontract.Answer {\n return nc_shared_RequestDeadlineExceeded(nc_fields_RequestDeadlineExceeded{route = route, timeout_milliseconds = timeout_milliseconds}, {})\n}\n\n@(private, require_results)\nnc_completion_unavailable :: proc(route: string, operation: string, errno: i32) -> nocontract.Answer {\n return nc_shared_RequestCompletionUnavailable(nc_fields_RequestCompletionUnavailable{route = route, operation = operation, errno = errno}, {})\n}\n\n// serve blocks on this thread until odin-http finishes its interrupt shutdown.\n@(require_results)\nserve :: proc(options: nocontract.Listen_Options) -> nocontract.Server_Terminal {\n return nocontract.serve(options, nocontract.Binding{select = nc_select, deadline_exceeded = nc_deadline_exceeded, completion_unavailable = nc_completion_unavailable})\n}\n")
	return outcome.FoldOption(outcome.EmptyableToOption(errors.Join(writer.problems...)), outcome.Err[[]native.File], func() outcome.Result[[]native.File] {
		writer.coreImport("fmt")
		writer.coreImport("log")
		header := &strings.Builder{}
		header.WriteString(description.GeneratedHeader + "\npackage nocontract_server\n\nimport nocontract \"nocontract:odin/runtime\"\n")
		slices.Sort(writer.core)
		for _, name := range writer.core {
			fmt.Fprintf(header, "import %s\n", strconv.Quote("core:"+name))
		}
		for index, directory := range writer.imports {
			relative := outcome.Fold(outcome.TupleToResult(filepath.Rel(request.Server, directory)), func(path string) string { return path }, func(cause error) string {
				writer.problems = append(writer.problems, compiler.ImportUnresolved{From: request.Server, Package: directory, Cause: cause})
				return ""
			})
			fmt.Fprintf(header, "import nc_package_%d %s\n", index, strconv.Quote(filepath.ToSlash(relative)))
		}
		for index, values := range writer.literals {
			fmt.Fprintf(header, "\n@(private)\nnc_strings_%d := %s\n", index, odinStrings(values))
		}
		return outcome.FoldOption(outcome.EmptyableToOption(errors.Join(writer.problems...)), outcome.Err[[]native.File], func() outcome.Result[[]native.File] {
			return outcome.Ok([]native.File{{Path: filepath.Join(request.Server, "nocontract_bindings.odin"), Contents: []byte(header.String() + writer.source.String())}})
		})
	})
}

func (writer *bindingWriter) literalValues(values []string) string {
	for index, existing := range writer.literals {
		if slices.Equal(existing, values) {
			return fmt.Sprintf("nc_strings_%d", index)
		}
	}
	writer.literals = append(writer.literals, slices.Clone(values))
	return fmt.Sprintf("nc_strings_%d", len(writer.literals)-1)
}

func (writer *bindingWriter) coreImport(name string) {
	if !slices.Contains(writer.core, name) {
		writer.core = append(writer.core, name)
	}
}

func (writer *bindingWriter) refuse(declaration, rule string) string {
	writer.problems = append(writer.problems, DeclarationRefused{Position: writer.request.API.Position, Declaration: declaration, Rule: rule})
	return ""
}

func (writer *bindingWriter) base(id int) compiler.Type { return compiler.Base(writer.graph, id) }

func (writer *bindingWriter) importDirectory(directory string) int {
	if found := slices.Index(writer.imports, directory); found >= 0 {
		return found
	}
	writer.imports = append(writer.imports, directory)
	return len(writer.imports) - 1
}

func (writer *bindingWriter) unsupported(id int, operation string) string {
	return writer.refuse(fmt.Sprintf("%T", writer.graph.Types[id]), "unsupported checked native type while generating "+operation)
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
	return writer.unsupported(id, "native value construction")
}
