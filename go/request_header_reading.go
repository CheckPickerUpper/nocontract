package nocontract

import (
	"net/http"
	"strings"
)

// Header is the declared header's combined value, refused when no field line carries it.
func Header(request *http.Request, name string) Text {
	canonical := strings.ToLower(name)
	values := request.Header.Values(name)
	if len(values) == 0 {
		return Text{part: RequestRefusedPartHeader, name: canonical, reading: RequestRefused{
			Part: RequestRefusedPartHeader, Name: canonical, Reason: "the header is missing",
		}}
	}
	return Text{part: RequestRefusedPartHeader, name: canonical, reading: textPresent{text: strings.Join(values, ", ")}}
}
