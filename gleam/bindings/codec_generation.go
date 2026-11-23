package bindings

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/outcome"
)

type generator struct {
	records  map[compiler.Identity]Record
	members  map[string]description.Member
	imports  map[string]string
	decoders map[string]string
	encoders map[string]string
	outcomes map[string]string
	problems map[string]error
}

// TypeUnresolved is a compiler type without a native codec representation.
type TypeUnresolved struct{ Type compiler.Type }

func (failure TypeUnresolved) Error() string {
	return fmt.Sprintf("native codec has no resolved type for %+v", failure.Type)
}

// ValueUnresolved is a missing checked native value.
type ValueUnresolved struct{ Value Value }

func (failure ValueUnresolved) Error() string {
	return fmt.Sprintf("native codec has no checked value for %+v", failure.Value)
}

// AnswerUnresolved is a missing branch of a named outcome.
type AnswerUnresolved struct{ Type compiler.Named }

func (failure AnswerUnresolved) Error() string {
	return fmt.Sprintf("native outcome has no checked branch for %+v", failure.Type)
}

func (generated generator) module(name string) string {
	return outcome.FoldOption(keyed.Entry(generated.imports, name), func(alias string) string { return alias }, func() string {
		alias := nativeModuleAlias(name, len(generated.imports))
		generated.imports[name] = alias
		return alias
	})
}

func nativeModuleAlias(name string, index int) string {
	switch name {
	case "gleam/dict":
		return "dict"
	case "gleam/option":
		return "option"
	case "gleam/list":
		return "list"
	case "gleam/result":
		return "result"
	case "gleam/int":
		return "int"
	case "nocontract_runtime/parameter_reading":
		return "parameter"
	}
	return fmt.Sprintf("native_%d", index)
}

func (generated generator) typeName(resolved compiler.Type) string {
	switch typed := resolved.(type) {
	case compiler.Named:
		name := typed.Identity.Name
		if typed.Identity.Module != "gleam" {
			name = generated.module(typed.Identity.Module) + "." + name
		}
		arguments := []string{}
		for _, argument := range typed.Arguments {
			arguments = append(arguments, generated.typeName(argument))
		}
		if len(arguments) > 0 {
			return name + "(" + strings.Join(arguments, ", ") + ")"
		}
		return name
	case compiler.Tuple:
		elements := []string{}
		for _, element := range typed.Elements {
			elements = append(elements, generated.typeName(element))
		}
		return "#(" + strings.Join(elements, ", ") + ")"
	case compiler.Variable, compiler.FunctionType, nil:
	}
	generated.problems[fmt.Sprintf("%+v", resolved)] = TypeUnresolved{Type: resolved}
	return ""
}

func (generated generator) nativeType(value Value) compiler.Type {
	switch typed := value.(type) {
	case Text, Timestamp:
		return primitiveType("String")
	case Whole:
		return primitiveType("Int")
	case Number:
		return primitiveType("Float")
	case Truth:
		return primitiveType("Bool")
	case Record:
		return typed.Native
	case RecordRef:
		return typed.Native
	case Enumeration:
		return typed.Native
	case List:
		return typed.Native
	case Fixed:
		return typed.Native
	case Dictionary:
		return typed.Native
	case Nullable:
		return typed.Native
	case Wrapped:
		return typed.Native
	case nil:
	}
	generated.problems["missing value"] = ValueUnresolved{Value: value}
	return compiler.Variable{ID: -1}
}

func primitiveType(name string) compiler.Named {
	return compiler.Named{Identity: compiler.Identity{Package: "", Module: "gleam", Name: name}, Arguments: []compiler.Type{}}
}

func codecName(prefix, typeName string) string {
	return fmt.Sprintf("%s_%x", prefix, sha256.Sum256([]byte(typeName)))[:len(prefix)+17]
}
func quoted(text string) string { return strconv.Quote(text) }

func constructorCall(module string, constructor compiler.Constructor, values []string) string {
	name := module + "." + constructor.Name
	if len(values) == 0 {
		return name
	}
	arguments := []string{}
	for i, parameter := range constructor.Parameters {
		arguments = append(arguments, outcome.FoldOption(parameter.Label, func(label string) string { return label + ": " + values[i] }, func() string { return values[i] }))
	}
	return name + "(" + strings.Join(arguments, ", ") + ")"
}

func (generated generator) decoder(value Value) string {
	typeName := generated.typeName(generated.nativeType(value))
	name := codecName("decode", typeName)
	if _, exists := generated.decoders[name]; !exists {
		generated.decoders[name] = ""
		body := generated.decodeBody(value)
		generated.decoders[name] = "fn " + name + "() -> decode.Decoder(" + typeName + ") {\n" + body + "\n}\n"
	}
	if _, reference := value.(RecordRef); reference {
		return "decode.recursive(" + name + ")"
	}
	return name + "()"
}

func (generated generator) recordCodec(reference RecordRef, emit func(Value) string) string {
	identity := reference.Native.Identity
	return outcome.FoldOption(keyed.Entry(generated.records, identity), func(record Record) string {
		return emit(record)
	}, func() string {
		generated.problems[fmt.Sprintf("%+v", identity)] = ValueUnresolved{Value: reference}
		return ""
	})
}

func (generated generator) decodeBody(value Value) string {
	switch typed := value.(type) {
	case Text, Timestamp:
		return "decode.string"
	case Whole:
		return "decode.int"
	case Truth:
		return "decode.bool"
	case Number:
		return "decode.float"
	case RecordRef:
		return generated.recordCodec(typed, generated.decodeBody)
	case Record:
		lines, values := []string{}, []string{}
		for i, field := range typed.Fields {
			local := fmt.Sprintf("field_%d", i)
			lines = append(lines, "use "+local+" <- decode.field("+quoted(field.Name.Text())+", "+generated.decoder(field.Value)+")")
			values = append(values, local)
		}
		return strings.Join(append(lines, "decode.success("+constructorCall(generated.module(typed.Native.Identity.Module), typed.Constructor, values)+")"), "\n")
	case Enumeration:
		arms := []string{}
		module := generated.module(typed.Native.Identity.Module)
		for _, literal := range typed.Literals {
			arms = append(arms, quoted(literal.Text)+" -> decode.success("+module+"."+literal.Constructor+")")
		}
		arms = append(arms, "_ -> decode.failure("+module+"."+typed.Literals[0].Constructor+", expected: "+quoted("one of "+literalNames(typed.Literals))+")")
		return "use text <- decode.then(decode.string)\ncase text {\n" + strings.Join(arms, "\n") + "\n}"
	case List:
		return "decode.list(" + generated.decoder(typed.Item) + ")"
	case Fixed:
		lines := []string{"use elements <- decode.then(decode.list(decode.dynamic))"}
		values, pattern := []string{}, []string{}
		for i := range len(typed.Native.Elements) {
			local := fmt.Sprintf("element_%d", i)
			lines = append(lines, fmt.Sprintf("use %s <- decode.subfield([%d], %s)", local, i, generated.decoder(typed.Item)))
			values, pattern = append(values, local), append(pattern, "_")
		}
		tuple := "#(" + strings.Join(values, ", ") + ")"
		lines = append(lines, "case elements {\n["+strings.Join(pattern, ", ")+"] -> decode.success("+tuple+")\n_ -> decode.failure("+tuple+", expected: "+quoted(fmt.Sprintf("an array of exactly %d elements", len(values)))+")\n}")
		return strings.Join(lines, "\n")
	case Dictionary:
		return "decode.dict(" + generated.decoder(typed.Key) + ", " + generated.decoder(typed.Item) + ")"
	case Nullable:
		return "decode.optional(" + generated.decoder(typed.Inner) + ")"
	case Wrapped:
		return "use inner <- decode.then(" + generated.decoder(typed.Inner) + ")\ndecode.success(" + constructorCall(generated.module(typed.Native.Identity.Module), typed.Constructor, []string{"inner"}) + ")"
	case nil:
	}
	generated.problems["missing decoder"] = ValueUnresolved{Value: value}
	return ""
}

func literalNames(literals []Literal) string {
	names := []string{}
	for _, literal := range literals {
		names = append(names, literal.Text)
	}
	return strings.Join(names, ", ")
}

func (generated generator) encoder(value Value) string {
	typeName := generated.typeName(generated.nativeType(value))
	key := typeName
	if whole, isWhole := value.(Whole); isWhole {
		key += ":" + whole.Width.Least() + ":" + whole.Width.Most()
	}
	name := codecName("encode", key)
	if _, exists := generated.encoders[name]; exists {
		return name
	}
	generated.encoders[name] = ""
	body := generated.encodeBody(value)
	generated.encoders[name] = "fn " + name + "(value: " + typeName + ") -> json.Json {\n" + body + "\n}\n"
	return name
}

func (generated generator) encodeBody(value Value) string {
	switch typed := value.(type) {
	case Text, Timestamp:
		return "json.string(value)"
	case Whole:
		switch typed.Width.Wire().(type) {
		case description.DecimalTextWire:
			generated.module("gleam/int")
			return "value |> int.to_string |> json.string"
		case description.NumberWire:
			return "json.int(value)"
		case nil:
		}
	case Truth:
		return "json.bool(value)"
	case Number:
		return "json.float(value)"
	case RecordRef:
		return generated.recordCodec(typed, generated.encodeBody)
	case Record:
		fields := []string{}
		for _, field := range typed.Fields {
			fields = append(fields, "#("+quoted(field.Name.Text())+", "+generated.encoder(field.Value)+"(value."+field.Name.Text()+"))")
		}
		return "json.object([" + strings.Join(fields, ", ") + "])"
	case Enumeration:
		return "json.string(" + generated.keyEncoder(typed) + "(value))"
	case List:
		return "json.array(value, of: " + generated.encoder(typed.Item) + ")"
	case Fixed:
		values, encoded := []string{}, []string{}
		for i := range len(typed.Native.Elements) {
			local := fmt.Sprintf("element_%d", i)
			values = append(values, local)
			encoded = append(encoded, generated.encoder(typed.Item)+"("+local+")")
		}
		return "let #(" + strings.Join(values, ", ") + ") = value\njson.preprocessed_array([" + strings.Join(encoded, ", ") + "])"
	case Dictionary:
		generated.module("gleam/list")
		key := "key"
		if enumeration, isEnumeration := typed.Key.(Enumeration); isEnumeration {
			key = generated.keyEncoder(enumeration) + "(key)"
		}
		return "value\n|> dict.to_list\n|> list.map(fn(entry) { let #(key, item) = entry #(" + key + ", " + generated.encoder(typed.Item) + "(item)) })\n|> json.object"
	case Nullable:
		return "case value {\noption.Some(inner) -> " + generated.encoder(typed.Inner) + "(inner)\noption.None -> json.null()\n}"
	case Wrapped:
		return "let " + constructorCall(generated.module(typed.Native.Identity.Module), typed.Constructor, []string{"inner"}) + " = value\n" + generated.encoder(typed.Inner) + "(inner)"
	case nil:
	}
	generated.problems["missing encoder"] = ValueUnresolved{Value: value}
	return ""
}

func (generated generator) keyEncoder(value Enumeration) string {
	typeName := generated.typeName(value.Native)
	name := codecName("key", typeName)
	if _, exists := generated.encoders[name]; exists {
		return name
	}
	arms := []constructorBranch{}
	for _, literal := range value.Literals {
		arms = append(arms, constructorBranch{pattern: generated.module(value.Native.Identity.Module) + "." + literal.Constructor, expression: quoted(literal.Text)})
	}
	generated.encoders[name] = "fn " + name + "(value: " + typeName + ") -> String {\n" + constructorExpression("value", arms) + "\n}\n"
	return name
}

type constructorBranch struct {
	pattern    string
	expression string
}

func constructorExpression(value string, branches []constructorBranch) string {
	if len(branches) == 1 {
		return "let " + branches[0].pattern + " = " + value + "\n" + branches[0].expression
	}
	arms := []string{}
	for _, branch := range branches {
		arms = append(arms, branch.pattern+" -> "+branch.expression)
	}
	return "case " + value + " {\n" + strings.Join(arms, "\n") + "\n}"
}

func (generated generator) outcomeEncoder(value Outcome) string {
	typeName := generated.typeName(value.Native)
	name := codecName("answer", typeName)
	if _, exists := generated.outcomes[name]; exists {
		return name
	}
	arms := []constructorBranch{}
	for _, answer := range value.Answers {
		switch branch := answer.(type) {
		case Direct:
			fields, locals := []string{}, []string{}
			for i, field := range branch.Fields {
				local := fmt.Sprintf("field_%d", i)
				locals = append(locals, local)
				fields = append(fields, "#("+quoted(field.Name.Text())+", "+generated.encoder(field.Value)+"("+local+"))")
			}
			pattern := constructorCall(generated.module(value.Native.Identity.Module), branch.Constructor, locals)
			member, exists := generated.members[branch.Constructor.Name]
			if !exists {
				generated.problems[branch.Constructor.Name] = AnswerUnresolved{Type: value.Native}
				continue
			}
			status, code := 0, "option.None"
			envelope := []string{"#(\"_tag\", json.string(" + quoted(member.Name) + "))"}
			schemas := []string{"#(\"_tag\", wire.LiteralsSchema(values: [" + quoted(member.Name) + "]))"}
			switch answer := member.Answer.(type) {
			case description.Succeeds:
				status = answer.Status.Code()
			case description.Fails:
				status = answer.Status.Code()
				code = "option.Some(" + quoted(answer.Code.String()) + ")"
				envelope = append(envelope, "#(\"code\", json.string("+quoted(answer.Code.String())+"))")
				schemas = append(schemas, "#(\"code\", wire.LiteralsSchema(values: ["+quoted(answer.Code.String())+"]))")
			case nil:
			}
			for _, field := range member.Fields {
				schemas = append(schemas, "#("+quoted(field.Name.Text())+", "+generated.schema(field.Schema)+")")
			}
			arms = append(arms, constructorBranch{pattern: pattern, expression: fmt.Sprintf("write_answer(tag: %s, status: %d, code: %s, fields: json.object([%s]), schema: wire.RecordSchema(fields: [%s]))", quoted(member.Name), status, code, strings.Join(append(envelope, fields...), ", "), strings.Join(schemas, ", "))})
		case Forwarded:
			pattern := constructorCall(generated.module(value.Native.Identity.Module), branch.Constructor, []string{"inner"})
			arms = append(arms, constructorBranch{pattern: pattern, expression: generated.outcomeEncoder(branch.Inner) + "(inner)"})
		case nil:
			generated.problems["missing outcome"] = AnswerUnresolved{Type: value.Native}
		}
	}
	generated.outcomes[name] = "fn " + name + "(value: " + typeName + ") -> Answer {\n" + constructorExpression("value", arms) + "\n}\n"
	return name
}
