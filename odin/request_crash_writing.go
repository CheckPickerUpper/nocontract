package odin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/outcome"
)

// crashResponse is the whole HTTP response a crashed request sends, written before dispatch so the
// signal handler only copies bytes.
func (writer *bindingWriter) crashResponse(route outcome.Option[description.Endpoint]) string {
	crash := description.RequestCrashed()
	selected := outcome.FoldOption(route, func(endpoint description.Endpoint) any {
		return map[string]string{crash.RouteMethod.Text(): endpoint.Method.Name(), crash.RoutePath.Text(): description.PathTemplate(endpoint.Path)}
	}, func() any { return nil })
	values := map[string]any{
		description.TagField().Text():  crash.Failure.Name,
		description.CodeField().Text(): crash.Failure.Code.String(),
		crash.Route.Text():             selected,
	}
	return outcome.Fold(outcome.TupleToResult(json.Marshal(values)), func(body []byte) string {
		status := crash.Failure.Status.Code()
		return strconv.Quote(fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, http.StatusText(status), len(body), body))
	}, func(cause error) string {
		writer.problems = append(writer.problems, cause)
		return ""
	})
}

func (writer *bindingWriter) crashDeclarations(bindings []endpointBinding) {
	fmt.Fprintf(writer.source, "\n@(private)\nnc_crash_unmatched :: %s\n", writer.crashResponse(outcome.None[description.Endpoint]()))
	for position, binding := range bindings {
		fmt.Fprintf(writer.source, "\n@(private)\nnc_crash_%d :: %s\n", position, writer.crashResponse(outcome.Some(binding.endpoint)))
	}
	writer.source.WriteString("\n@(private, require_results)\nnc_protection_unavailable :: proc(refusal: nocontract.Request_Protection_Unavailable) -> nocontract.Answer {\n return nc_shared_AnswerNotWritten(nc_fields_AnswerNotWritten{answer = \"request protection\", reason = fmt.tprintf(\"the %d-byte request signal stack was not installed: %v\", refusal.bytes, refusal.cause)}, {})\n}\n")
}
