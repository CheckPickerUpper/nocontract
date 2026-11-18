package reading

import (
	"go/token"
	"go/types"
	"strings"

	"github.com/CheckPickerUpper/outcome"
	"github.com/fatih/structtag"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

// StructTagMalformed is a field whose tag does not follow Go's key:"value" convention, so
// reflect, and encoding/json with it, reads no key of it.
//
//ergo:code NC-1110
//ergo:message The tag of the field {Field}, {Tag}, is not written key:"value" with a space between pairs.
type StructTagMalformed struct {
	Field string
	Tag   string
	Cause error
}

// FieldNameRepeated is a field written under a name an earlier field of its struct takes. Two JSON
// fields under one name make encoding/json write neither; two parameters under one name read one
// value twice.
//
//ergo:code NC-1111
//ergo:message The field {Field} is written under the name {Name}, which the field {First} already takes.
type FieldNameRepeated struct {
	Field string
	Name  string
	First string
}

func (StructTagMalformed) isProblem() {}
func (FieldNameRepeated) isProblem()  {}

// tagValue is one key's value in a field's tag: its name, before the first comma, and each option
// after it.
type tagValue struct {
	text    string
	name    string
	options []string
}

// fieldTags is each key's value in a field's tag, read as reflect reads it.
type fieldTags struct{ values map[string]tagValue }

// lookup is the value the tag gives key, when it gives one.
func (tags fieldTags) lookup(key string) outcome.Option[tagValue] {
	return keyed.Entry(tags.values, key)
}

// readFieldTags reads a field's tag, refusing one reflect would read no key of.
func readFieldTags(index projectIndex, field indexedField) reading[fieldTags] {
	// structtag reads a tag of nothing but spaces as no tags at all, a nil *Tags.
	if strings.Trim(field.tag, " ") == "" {
		return readAs(fieldTags{values: map[string]tagValue{}}, nothingFound())
	}
	return outcome.Fold(outcome.TupleToResult(structtag.Parse(field.tag)), func(parsed *structtag.Tags) reading[fieldTags] {
		values := map[string]tagValue{}
		for _, tag := range parsed.Tags() {
			// reflect gives a key repeated in one tag its first value.
			if _, isTaken := values[tag.Key]; !isTaken {
				values[tag.Key] = tagValue{text: tag.Value(), name: tag.Name, options: tag.Options}
			}
		}
		return readAs(fieldTags{values: values}, nothingFound())
	}, func(cause error) reading[fieldTags] {
		return notRead[fieldTags](index.refused(field.variable.Pos(), StructTagMalformed{Field: field.variable.Name(), Tag: field.tag, Cause: cause}))
	})
}

// readFieldName reads text as the name a field is written under, refusing it where position is.
func readFieldName(index projectIndex, position token.Pos, text string) reading[description.FieldName] {
	switch read := description.ParseFieldName(text).(type) {
	case description.FieldNameRead:
		return readAs(read.Name, nothingFound())
	case description.FieldNameRefused:
		return notRead[description.FieldName](index.refused(position, read.Problem))
	case nil:
	}
	return notRead[description.FieldName](nothingFound())
}

// writtenName is the name a Go field is written under.
type writtenName struct {
	name     description.FieldName
	variable *types.Var
}

// namedOnce is items when no two of them are written under one name, or a refusal of each one
// written under a name an earlier one takes.
func namedOnce[T any](index projectIndex, items []T, written func(T) writtenName) reading[[]T] {
	first := map[description.FieldName]writtenName{}
	repeated := nothingFound()
	for _, item := range items {
		each := written(item)
		outcome.FoldOption(keyed.Entry(first, each.name), func(taken writtenName) struct{} {
			repeated = repeated.with(index.refused(each.variable.Pos(), FieldNameRepeated{
				Field: each.variable.Name(), Name: each.name.Text(), First: taken.variable.Name(),
			}))
			return struct{}{}
		}, func() struct{} {
			first[each.name] = each
			return struct{}{}
		})
	}
	if len(repeated.refusals) > 0 {
		return notRead[[]T](repeated)
	}
	return readAs(items, nothingFound())
}
