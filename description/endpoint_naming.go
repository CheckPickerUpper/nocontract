package description

import (
	"unicode"
	"unicode/utf8"
)

// EndpointName uses a camel case handler's name with a lowercase initial.
func EndpointName(handler string) string {
	first, size := utf8.DecodeRuneInString(handler)
	return string(unicode.ToLower(first)) + handler[size:]
}

// OperationID qualifies an endpoint by the group declaring it.
func OperationID(group, endpoint string) string { return group + "." + endpoint }
