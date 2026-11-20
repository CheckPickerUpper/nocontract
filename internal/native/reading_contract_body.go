package native

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strconv"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	nocontract "github.com/CheckPickerUpper/nocontract/go"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

type bodyReader func(nocontract.BodyValue) nocontract.BodyReading[json.RawMessage]

//ergo:not-an-error
type bodyObject map[string]json.RawMessage

type bodyReaders struct {
	records    map[string]bodyReader
	signed64   bodyReader
	unsigned64 bodyReader
}

func requestBodies(records []description.NamedStruct) bodyReaders {
	return contractBodies(records, encodedBody(nocontract.BodySignedText[int64]), encodedBody(nocontract.BodyUnsignedText[uint64]))
}

func answerBodies(records []description.NamedStruct) bodyReaders {
	return contractBodies(records,
		bodyAfter(nocontract.BodySigned[int64], func(value int64) string { return strconv.FormatInt(value, 10) }),
		bodyAfter(nocontract.BodyUnsigned[uint64], func(value uint64) string { return strconv.FormatUint(value, 10) }),
	)
}

func contractBodies(records []description.NamedStruct, signed64, unsigned64 bodyReader) bodyReaders {
	readers := bodyReaders{records: make(map[string]bodyReader, len(records)), signed64: signed64, unsigned64: unsigned64}
	for _, record := range records {
		readers.records[record.Name] = encodedBody(recordBody(readers, record.Fields))
	}
	return readers
}

func encodeBody[V any](value V) nocontract.BodyReading[json.RawMessage] {
	return outcome.Fold(outcome.TupleToResult(jsonv2.Marshal(value)), func(encoded []byte) nocontract.BodyReading[json.RawMessage] {
		return nocontract.Read[json.RawMessage]{Value: json.RawMessage(encoded)}
	}, func(cause error) nocontract.BodyReading[json.RawMessage] {
		return nocontract.BodyRefused{Cause: nocontract.BodyNotReceived{Reason: cause.Error()}}
	})
}

func encodedBody[V any](read func(nocontract.BodyValue) nocontract.BodyReading[V]) bodyReader {
	return func(value nocontract.BodyValue) nocontract.BodyReading[json.RawMessage] {
		return nocontract.Next(read(value), encodeBody[V])
	}
}

func bodyAfter[V, W any](read func(nocontract.BodyValue) nocontract.BodyReading[V], transform func(V) W) bodyReader {
	return func(value nocontract.BodyValue) nocontract.BodyReading[json.RawMessage] {
		return nocontract.Next(read(value), func(held V) nocontract.BodyReading[json.RawMessage] {
			return encodeBody(transform(held))
		})
	}
}

func compiledBody(readers bodyReaders, schema description.Schema) bodyReader {
	switch shape := schema.(type) {
	case description.Text:
		return encodedBody(nocontract.BodyText[string])
	case description.Integer:
		return wholeBody(readers, shape.Width)
	case description.Finite:
		return encodedBody(nocontract.BodyNumber[float64])
	case description.Truth:
		return encodedBody(nocontract.BodyTruth[bool])
	case description.Instant:
		return encodedBody(nocontract.BodyInstant)
	case description.Literals:
		return encodedBody(nocontract.BodyLiteral(shape.Values...))
	case description.List:
		return encodedBody(nocontract.BodyList[[]json.RawMessage](compiledBody(readers, shape.Item)))
	case description.FixedList:
		return encodedBody(nocontract.BodyArray(int(shape.Length), compiledBody(readers, shape.Item), func(items []json.RawMessage) []json.RawMessage {
			return items
		}))
	case description.Dictionary:
		return encodedBody(nocontract.BodyDictionary[bodyObject](dictionaryKey(shape.Key), compiledBody(readers, shape.Value)))
	case description.Nullable:
		return nocontract.BodyNullableAs(compiledBody(readers, shape.Inner), func(value json.RawMessage) json.RawMessage {
			return value
		}, func() json.RawMessage { return json.RawMessage("null") })
	case description.StructRef:
		return func(value nocontract.BodyValue) nocontract.BodyReading[json.RawMessage] {
			return outcome.FoldOption(keyed.Entry(readers.records, shape.Name), func(read bodyReader) nocontract.BodyReading[json.RawMessage] {
				return read(value)
			}, func() nocontract.BodyReading[json.RawMessage] {
				return nocontract.BodyRefused{Cause: nocontract.BodyNotReceived{Reason: fmt.Sprintf("record %q was not declared", shape.Name)}}
			})
		}
	case description.InlineStruct:
		return encodedBody(recordBody(readers, shape.Fields))
	case nil:
	}
	return unreadBody("body schema is missing")
}

func unreadBody(reason string) bodyReader {
	return func(nocontract.BodyValue) nocontract.BodyReading[json.RawMessage] {
		return nocontract.BodyRefused{Cause: nocontract.BodyNotReceived{Reason: reason}}
	}
}

func wholeBody(readers bodyReaders, width description.IntegerWidth) bodyReader {
	switch width.(type) {
	case description.Signed8:
		return encodedBody(nocontract.BodySigned[int8])
	case description.Signed16:
		return encodedBody(nocontract.BodySigned[int16])
	case description.Signed32:
		return encodedBody(nocontract.BodySigned[int32])
	case description.Signed64:
		return readers.signed64
	case description.Unsigned8:
		return encodedBody(nocontract.BodyUnsigned[uint8])
	case description.Unsigned16:
		return encodedBody(nocontract.BodyUnsigned[uint16])
	case description.Unsigned32:
		return encodedBody(nocontract.BodyUnsigned[uint32])
	case description.Unsigned64:
		return readers.unsigned64
	case nil:
	}
	return unreadBody("whole number width is missing")
}

func dictionaryKey(key description.DictionaryKey) func(nocontract.BodyMember) nocontract.BodyReading[string] {
	switch keys := key.(type) {
	case description.Text:
		return nocontract.BodyTextKey[string]
	case description.Literals:
		return nocontract.BodyLiteralKey(keys.Values...)
	case nil:
	}
	return func(nocontract.BodyMember) nocontract.BodyReading[string] {
		return nocontract.BodyRefused{Cause: nocontract.BodyNotReceived{Reason: "dictionary key schema is missing"}}
	}
}

func recordBody(readers bodyReaders, fields []description.Field) func(nocontract.BodyValue) nocontract.BodyReading[map[string]json.RawMessage] {
	members := make([]nocontract.BodyField[json.RawMessage], 0, len(fields))
	for _, field := range fields {
		members = append(members, nocontract.BodyField[json.RawMessage]{Name: field.Name.Text(), Read: compiledBody(readers, field.Schema)})
	}
	return nocontract.BodyRecord(members)
}
