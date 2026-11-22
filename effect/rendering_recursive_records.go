package effect

import (
	"fmt"

	"github.com/CheckPickerUpper/nocontract/description"
)

// namedStructText declares the decoded and encoded types from the same fields as the codec.
// The typed boundary and suspended references permit forward and recursive references alike.
func namedStructText(named description.NamedStruct, prefix string) string {
	fields := schemaFields(named.Fields, prefix, "")
	encoded := prefix + "Encoded_" + named.Name
	return fmt.Sprintf("\nexport type %s = %s\ntype %s = %s\nexport const %s: %sSchema.Codec<%s, %s> = %sSchema.Struct(%s)\n", named.Name, fields.decoded, encoded, fields.encoded, named.Name, prefix, named.Name, encoded, prefix, fields.codec)
}
