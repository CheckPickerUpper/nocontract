package odin

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
	"github.com/CheckPickerUpper/outcome"
)

func (writer *bindingWriter) routeDeclaration(position int, binding endpointBinding) {
	fmt.Fprintf(writer.source, "\n@(private, require_results)\nnc_route_%d :: proc(request: nocontract.Request, received: nocontract.Received_Request, parameters: map[string]string) -> nocontract.Answer {\n", position)
	outcome.ForEachOption(binding.native.guard, func(guard guardBinding) {
		fmt.Fprintf(writer.source, " switch guarded in nc_package_%d.%s(%s, request.headers) {\n", writer.importDirectory(filepath.Dir(guard.declaration.File)), guard.declaration.Entity.Name, strconv.Quote(guard.scope))
		for _, variant := range guard.output.Variants {
			fmt.Fprintf(writer.source, " case %s:\n", writer.nativeType(variant))
			outcome.FoldOption(findAdmitted(writer.graph, variant), func(compiler.Named) struct{} { return struct{}{} }, func() struct{} {
				fmt.Fprintf(writer.source, " return nc_answer_%d(guarded)\n", variant)
				return struct{}{}
			})
		}
		if !guard.output.NoNil {
			fmt.Fprintf(writer.source, " case nil: return nc_shared_AnswerNotWritten(nc_fields_AnswerNotWritten{answer = %s, reason = \"the guard returned no decision\"}, {})\n", strconv.Quote(guard.declaration.Entity.Name))
		}
		writer.source.WriteString(" }\n")
	})
	fmt.Fprintf(writer.source, " switch input in nc_input_%d(request, received, parameters) {\n case nocontract.Read_Value(%s):\n switch answer in nc_package_%d.%s(input.value) {\n", position, writer.nativeType(binding.native.input), writer.importDirectory(filepath.Dir(binding.native.declaration.File)), binding.native.declaration.Entity.Name)
	for _, variant := range binding.native.output.Variants {
		fmt.Fprintf(writer.source, " case %s: return nc_answer_%d(answer)\n", writer.nativeType(variant), variant)
	}
	if !binding.native.output.NoNil {
		writer.source.WriteString(" case nil: return nc_shared_AnswerNotWritten(nc_fields_AnswerNotWritten{answer = \"\", reason = \"the route answered no outcome\"}, {})\n")
	}
	writer.source.WriteString(" }\n case nocontract.Request_Refusal: return nc_request_refused(input)\n }\n unreachable()\n}\n")
}

func pathShape(path []description.PathSegment) string {
	key := &strings.Builder{}
	for _, segment := range path {
		switch segment := segment.(type) {
		case description.PathText:
			key.WriteString("0:" + strconv.Quote(segment.Text) + "|")
		case description.PathParameter:
			key.WriteString("1:|")
		case nil:
		}
	}
	return key.String()
}

func (writer *bindingWriter) dispatcher(bindings []endpointBinding) {
	writer.coreImport("strings")
	ordered := make([]int, len(bindings))
	for index := range ordered {
		ordered[index] = index
	}
	slices.SortFunc(ordered, func(left, right int) int {
		return cmp.Or(cmp.Compare(pathShape(bindings[left].endpoint.Path), pathShape(bindings[right].endpoint.Path)), left-right)
	})
	groups := [][]int{}
	for _, index := range ordered {
		if len(groups) == 0 || pathShape(bindings[groups[len(groups)-1][0]].endpoint.Path) != pathShape(bindings[index].endpoint.Path) {
			groups = append(groups, []int{})
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], index)
	}
	writer.source.WriteString("\n@(private, require_results)\nnc_select :: proc(request: nocontract.Request) -> nocontract.Route_Plan {\n switch received in request.content {\n case nocontract.Received_Request:\n switch read in nocontract.read_request_path(request.path) {\n case nocontract.Read_Value(nocontract.Request_Path):\n segments := read.value.segments\n")
	for _, group := range groups {
		path := bindings[group[0]].endpoint.Path
		conditions := []string{fmt.Sprintf("len(segments) == %d", len(path))}
		methods := []string{}
		for position, segment := range path {
			switch segment := segment.(type) {
			case description.PathText:
				conditions = append(conditions, fmt.Sprintf("segments[%d] == %s", position, strconv.Quote(segment.Text)))
			case description.PathParameter, nil:
			}
		}
		fmt.Fprintf(writer.source, " if %s {\n switch received.method {\n", strings.Join(conditions, " && "))
		for _, index := range group {
			binding := bindings[index]
			methods = append(methods, binding.endpoint.Method.Name())
			fmt.Fprintf(writer.source, " case %s:\n parameters := make(map[string]string, context.temp_allocator)\n", strconv.Quote(binding.endpoint.Method.Name()))
			for position, segment := range binding.endpoint.Path {
				switch segment := segment.(type) {
				case description.PathParameter:
					fmt.Fprintf(writer.source, " parameters[%s] = segments[%d]\n", strconv.Quote(segment.Field.Name.Text()), position)
				case description.PathText, nil:
				}
			}
			fmt.Fprintf(writer.source, " return nocontract.Route_Plan{method = received.method, selection = nocontract.Selected_Route{template = %s, parameters = parameters, received = received, answer = nc_route_%d}}\n", strconv.Quote(description.PathTemplate(binding.endpoint.Path)), index)
		}
		slices.Sort(methods)
		fmt.Fprintf(writer.source, " }\n return nocontract.Route_Plan{method = received.method, selection = nocontract.Immediate_Answer{answer = nc_method_refused(received.method, %s)}}\n }\n", writer.literalValues(slices.Compact(methods)))
	}
	writer.source.WriteString(" decoded_path := strings.join(segments, \"/\", context.temp_allocator)\n if strings.has_prefix(request.path, \"/\") { decoded_path = fmt.tprintf(\"/%s\", decoded_path) }\n return nocontract.Route_Plan{method = received.method, selection = nocontract.Immediate_Answer{answer = nc_shared_RouteNotFound(nc_fields_RouteNotFound{method = received.method, path = decoded_path}, {})}}\n case nocontract.Request_Refusal: return nocontract.Route_Plan{method = received.method, selection = nocontract.Immediate_Answer{answer = nc_request_refused(read)}}\n }\n case nocontract.Request_Line_Not_Received:\n return nocontract.Route_Plan{method = \"\", selection = nocontract.Immediate_Answer{answer = nc_shared_AnswerNotWritten(nc_fields_AnswerNotWritten{answer = \"HTTP request\", reason = \"the HTTP library supplied no request line\"}, {})}}\n }\n unreachable()\n}\n")
}
