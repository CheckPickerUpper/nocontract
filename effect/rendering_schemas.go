package effect

import (
	"fmt"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
)

// renderedSchema derives the codec and both TypeScript representations in one traversal.
type renderedSchema struct {
	codec   string
	decoded string
	encoded string
}

func schemaFields(fields []description.Field, prefix, indent string) renderedSchema {
	codecs, decoded, encoded := []string{}, []string{}, []string{}
	for _, field := range fields {
		name := quoted(field.Name.Text())
		schema := renderSchema(field.Schema, prefix, indent+"  ")
		codecs = append(codecs, fmt.Sprintf("%s  [%s]: %s,", indent, name, schema.codec))
		decoded = append(decoded, "readonly "+name+": "+schema.decoded)
		encoded = append(encoded, "readonly "+name+": "+schema.encoded)
	}
	if len(fields) == 0 {
		return renderedSchema{codec: "{}", decoded: "{ readonly [key: string]: never }", encoded: "{ readonly [key: string]: never }"}
	}
	return renderedSchema{
		codec:   "{\n" + strings.Join(codecs, "\n") + "\n" + indent + "}",
		decoded: "{ " + strings.Join(decoded, "; ") + " }",
		encoded: "{ " + strings.Join(encoded, "; ") + " }",
	}
}

func renderSchema(schema description.Schema, prefix, indent string) renderedSchema {
	schemaNamespace := prefix + "Schema"
	switch typed := schema.(type) {
	case description.Text:
		return renderedSchema{codec: schemaNamespace + ".String", decoded: "string", encoded: "string"}
	case description.Integer:
		return renderInteger(typed.Width, prefix)
	case description.Finite:
		return renderedSchema{codec: schemaNamespace + ".Finite", decoded: "number", encoded: "number"}
	case description.Truth:
		return renderedSchema{codec: schemaNamespace + ".Boolean", decoded: "boolean", encoded: "boolean"}
	case description.Instant:
		return renderedSchema{codec: schemaNamespace + ".DateTimeUtcFromString", decoded: "typeof " + schemaNamespace + ".DateTimeUtcFromString.Type", encoded: "string"}
	case description.Literals:
		return renderLiterals(typed, prefix)
	case description.List:
		return renderList(renderSchema(typed.Item, prefix, indent), prefix)
	case description.FixedList:
		list := renderList(renderSchema(typed.Item, prefix, indent), prefix)
		return renderedSchema{codec: fmt.Sprintf("%s.check(%s.isBetweenLength(%d, %d))", list.codec, schemaNamespace, typed.Length, typed.Length), decoded: list.decoded, encoded: list.encoded}
	case description.Dictionary:
		return renderDictionary(typed, prefix, indent)
	case description.Nullable:
		inner := renderSchema(typed.Inner, prefix, indent)
		return renderedSchema{codec: schemaNamespace + ".NullOr(" + inner.codec + ")", decoded: "(" + inner.decoded + ") | null", encoded: "(" + inner.encoded + ") | null"}
	case description.StructRef:
		return renderedSchema{codec: schemaNamespace + ".suspend(() => " + typed.Name + ")", decoded: typed.Name, encoded: prefix + "Encoded_" + typed.Name}
	case description.InlineStruct:
		fields := schemaFields(typed.Fields, prefix, indent)
		return renderedSchema{codec: schemaNamespace + ".Struct(" + fields.codec + ")", decoded: fields.decoded, encoded: fields.encoded}
	case nil:
	}
	return renderedSchema{codec: schemaNamespace + ".Never", decoded: "never", encoded: "never"}
}

func renderList(item renderedSchema, prefix string) renderedSchema {
	return renderedSchema{codec: prefix + "Schema.Array(" + item.codec + ")", decoded: "readonly (" + item.decoded + ")[]", encoded: "readonly (" + item.encoded + ")[]"}
}

func renderDictionary(dictionary description.Dictionary, prefix, indent string) renderedSchema {
	value := renderSchema(dictionary.Value, prefix, indent)
	schemaNamespace := prefix + "Schema"
	switch key := dictionary.Key.(type) {
	case description.Literals:
		keys := renderLiterals(key, prefix)
		// A dictionary can omit any enum key; this is distinct from a nullable value.
		return renderedSchema{
			codec:   schemaNamespace + ".Record(" + keys.codec + ", " + schemaNamespace + ".optionalKey(" + value.codec + "))",
			decoded: "{ readonly [key in " + keys.decoded + "]?: " + value.decoded + " }",
			encoded: "{ readonly [key in " + keys.encoded + "]?: " + value.encoded + " }",
		}
	case description.Text, nil:
	}
	return renderedSchema{codec: schemaNamespace + ".Record(" + schemaNamespace + ".String, " + value.codec + ")", decoded: "{ readonly [key: string]: " + value.decoded + " }", encoded: "{ readonly [key: string]: " + value.encoded + " }"}
}

func renderLiterals(literals description.Literals, prefix string) renderedSchema {
	values := []string{}
	for _, value := range literals.Values {
		values = append(values, quoted(value))
	}
	codec := prefix + "Schema.Literals([" + strings.Join(values, ", ") + "])"
	text := strings.Join(values, " | ")
	if len(values) == 0 {
		text = "never"
	}
	return renderedSchema{codec: codec, decoded: text, encoded: text}
}

func renderInteger(width description.IntegerWidth, prefix string) renderedSchema {
	schemaNamespace := prefix + "Schema"
	switch wire := width.Wire().(type) {
	case description.NumberWire:
		return renderedSchema{codec: fmt.Sprintf("%s.Int.check(%s.isBetween({ minimum: %s, maximum: %s }))", schemaNamespace, schemaNamespace, width.Least(), width.Most()), decoded: "number", encoded: "number"}
	case description.DecimalTextWire:
		return renderedSchema{
			codec:   fmt.Sprintf("%[1]s.String.check(%[1]s.isPattern(/%[2]s/)).pipe(%[1]s.decodeTo(%[1]s.BigIntFromString.check(%[1]s.isBetweenBigInt({ minimum: %[3]sn, maximum: %[4]sn }))))", schemaNamespace, wire.Pattern, width.Least(), width.Most()),
			decoded: "bigint", encoded: "string",
		}
	case nil:
	}
	return renderedSchema{codec: schemaNamespace + ".Never", decoded: "never", encoded: "never"}
}
