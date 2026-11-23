package description

import "strings"

// PathTemplate writes a checked route path with each parameter as {name}.
func PathTemplate(segments []PathSegment) string {
	parts := []string{}
	for _, segment := range segments {
		switch written := segment.(type) {
		case PathText:
			parts = append(parts, written.Text)
		case PathParameter:
			parts = append(parts, "{"+written.Field.Name.Text()+"}")
		case nil:
		}
	}
	return "/" + strings.Join(parts, "/")
}
