package nocontract

import (
	"encoding/json/jsontext"
	"errors"
	"io"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/messages"
)

// A request's JSON body is read by a function the generated code writes for each struct the body
// reaches, built from the readers here: one for each JSON shape, and the object reader that reads a
// struct's members in the order the body writes them. Each value is read once, token by token, and
// refused with the cause that stopped it and where in the body it was.

// BodyReading is what reading one JSON value of a request's body as a V ended as.
//
//sumtype:decl
type BodyReading[V any] interface {
	isBodyReading()
}

// BodyRefused is a body refused for Cause.
//
//ergo:not-an-error
type BodyRefused struct{ Cause BodyCause }

func (Read[V]) isBodyReading()     {}
func (BodyRefused) isBodyReading() {}

// BodyCause is why a request's body was refused.
//
//sumtype:decl
type BodyCause interface {
	isBodyCause()
	Error() string
}

// BodyMemberMissing is a member the body's struct declares and the body does not hold.
//
//ergo:code NC-1114
//ergo:message {Path} is missing from the body.
type BodyMemberMissing struct{ Path string }

// BodyMemberRepeated is a member the body writes more than once in one object.
//
//ergo:code NC-1115
//ergo:message {Path} is written more than once.
type BodyMemberRepeated struct{ Path string }

// BodyMemberUnknown is a member the body writes that its struct does not declare.
//
//ergo:code NC-1116
//ergo:message {Path} is not a member the body accepts.
type BodyMemberUnknown struct{ Path string }

// BodyValueMistyped is a value of another JSON type than its field holds: the JSON type wanted,
// and the value read.
//
//ergo:code NC-1117
//ergo:message {Path} must hold {Want}, and it holds {Read}.
type BodyValueMistyped struct {
	Path string
	Want string
	Read string
}

// BodyLiteralOutside is a string that is none of the values its field accepts.
//
//ergo:code NC-1118
//ergo:message {Path} holds "{Read}", which is not one of the values it accepts.
type BodyLiteralOutside struct {
	Path   string
	Read   string
	Values []string
}

// BodyKeyOutside is an entry of a dictionary under a key that is none of the keys its field takes:
// the entry, its key, and the keys the field takes.
//
//ergo:code NC-1126
//ergo:message {Path} is under the key "{Read}", which is not one of the keys it accepts.
type BodyKeyOutside struct {
	Path string
	Read string
	Keys []string
}

// BodyNumberNotWhole is a number a whole number field holds, written with a fraction or an exponent.
//
//ergo:code NC-1119
//ergo:message {Path} holds {Read}, which is not written as a whole number.
type BodyNumberNotWhole struct {
	Path string
	Read string
}

// BodyNumberOutside is a number outside what its field's type holds: the number, and the least
// and the most that type holds.
//
//ergo:code NC-1120
//ergo:message {Path} holds {Read}, which is outside {Least} to {Most}.
type BodyNumberOutside struct {
	Path  string
	Read  string
	Least string
	Most  string
}

// BodyInstantUnreadable is a string an instant field holds that is not an RFC 3339 time, and why.
//
//ergo:code NC-1121
//ergo:message {Path} holds "{Read}", which is not an RFC 3339 time: {Reason}.
type BodyInstantUnreadable struct {
	Path   string
	Read   string
	Reason string
}

// BodyListLengthWrong is a list holding another number of items than its field's fixed length.
//
//ergo:code NC-1122
//ergo:message {Path} must hold exactly {Length} items, and it holds {Read}.
type BodyListLengthWrong struct {
	Path   string
	Length int
	Read   int
}

// BodyMalformed is a body that stops being JSON: the value being read, the byte where it stopped,
// and why.
//
//ergo:code NC-1123
//ergo:message {Path} is not JSON: {Reason}.
type BodyMalformed struct {
	Path   string
	Offset int64
	Reason string
}

// BodyEmpty is a body holding no JSON value.
//
//ergo:code NC-1124
//ergo:message The body holds no JSON value.
type BodyEmpty struct{}

// BodyNotReceived is a body the server could not read from the request, and why.
//
//ergo:code NC-1125
//ergo:message The body could not be read: {Reason}.
type BodyNotReceived struct{ Reason string }

// BodyNotSingle is a body holding more than one JSON value.
//
//ergo:code NC-1044
//ergo:message The body holds more than one JSON value.
type BodyNotSingle struct{}

// Error is the failure's message in the source language.
func (cause BodyMemberMissing) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyMemberRepeated) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyMemberUnknown) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyValueMistyped) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyLiteralOutside) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyKeyOutside) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyNumberNotWhole) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyNumberOutside) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyInstantUnreadable) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyListLengthWrong) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyMalformed) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyEmpty) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyNotReceived) Error() string { return cause.Report(messages.Match("")).Message }

// Error is the failure's message in the source language.
func (cause BodyNotSingle) Error() string { return cause.Report(messages.Match("")).Message }

func (BodyMemberMissing) isBodyCause()     {}
func (BodyMemberRepeated) isBodyCause()    {}
func (BodyMemberUnknown) isBodyCause()     {}
func (BodyValueMistyped) isBodyCause()     {}
func (BodyLiteralOutside) isBodyCause()    {}
func (BodyKeyOutside) isBodyCause()        {}
func (BodyNumberNotWhole) isBodyCause()    {}
func (BodyNumberOutside) isBodyCause()     {}
func (BodyInstantUnreadable) isBodyCause() {}
func (BodyListLengthWrong) isBodyCause()   {}
func (BodyMalformed) isBodyCause()         {}
func (BodyEmpty) isBodyCause()             {}
func (BodyNotReceived) isBodyCause()       {}
func (BodyNotSingle) isBodyCause()         {}

// bodyRoot is the path of the body's own value.
const bodyRoot = "body"

// bodyCausePath is where in the body cause was found: a value's path, or the body's own for a
// cause about the body as a whole.
func bodyCausePath(cause BodyCause) string {
	switch found := cause.(type) {
	case BodyMemberMissing:
		return found.Path
	case BodyMemberRepeated:
		return found.Path
	case BodyMemberUnknown:
		return found.Path
	case BodyValueMistyped:
		return found.Path
	case BodyLiteralOutside:
		return found.Path
	case BodyKeyOutside:
		return found.Path
	case BodyNumberNotWhole:
		return found.Path
	case BodyNumberOutside:
		return found.Path
	case BodyInstantUnreadable:
		return found.Path
	case BodyListLengthWrong:
		return found.Path
	case BodyMalformed:
		return found.Path
	case BodyEmpty, BodyNotReceived, BodyNotSingle, nil:
	}
	return bodyRoot
}

// BodyValue is the next JSON value of a request's body, and its path in the body, such as
// body.items[2].sku.
type BodyValue struct {
	decoder *jsontext.Decoder
	path    string
}

// member is the value of the member name of the object at value.
func (value BodyValue) member(name string) BodyValue {
	return BodyValue{decoder: value.decoder, path: value.path + "." + name}
}

// item is the item at index of the list at value.
func (value BodyValue) item(index int) BodyValue {
	return BodyValue{decoder: value.decoder, path: value.path + "[" + strconv.Itoa(index) + "]"}
}

// entry is the value under key of the dictionary at value.
func (value BodyValue) entry(key string) BodyValue {
	return BodyValue{decoder: value.decoder, path: value.path + "[" + strconv.Quote(key) + "]"}
}

// unread is the refusal of the body when reading value stopped with cause: JSON that stops being
// JSON, a body holding no value, or a body the server could not read.
func (value BodyValue) unread(cause error) BodyRefused {
	if syntactic, isSyntactic := errors.AsType[*jsontext.SyntacticError](cause); isSyntactic {
		return BodyRefused{Cause: BodyMalformed{Path: value.path, Offset: syntactic.ByteOffset, Reason: syntactic.Error()}}
	}
	if errors.Is(cause, io.EOF) {
		return BodyRefused{Cause: BodyEmpty{}}
	}
	return BodyRefused{Cause: BodyNotReceived{Reason: cause.Error()}}
}

// token is value's token when it is of one of kinds, which its field holds as want, or the refusal
// of a value of another JSON type, read whole so the refusal holds it.
func (value BodyValue) token(want string, kinds ...jsontext.Kind) BodyReading[jsontext.Token] {
	unread := func(cause error) BodyReading[jsontext.Token] { return value.unread(cause) }
	if kind := value.decoder.PeekKind(); kind != jsontext.KindInvalid && !slices.Contains(kinds, kind) {
		return mo.Fold(mo.TupleToResult(value.decoder.ReadValue()), func(read jsontext.Value) BodyReading[jsontext.Token] {
			return BodyRefused{Cause: BodyValueMistyped{Path: value.path, Want: want, Read: read.String()}}
		}, unread)
	}
	return mo.Fold(mo.TupleToResult(value.decoder.ReadToken()), func(read jsontext.Token) BodyReading[jsontext.Token] {
		return Read[jsontext.Token]{Value: read}
	}, unread)
}

// Next is next's reading of the value reading read, or the refusal when reading failed. The
// function generated for a struct chains one Next for each of its members.
func Next[V, W any](reading BodyReading[V], next func(V) BodyReading[W]) BodyReading[W] {
	switch read := reading.(type) {
	case Read[V]:
		return next(read.Value)
	case BodyRefused:
		return read
	case nil:
	}
	return BodyRefused{Cause: BodyNotReceived{Reason: "the value was not read"}}
}

// Body is the request's JSON body read by read, refused when it holds more than the one value read
// reads.
func Body[V any](request *http.Request, read func(BodyValue) BodyReading[V]) Reading[V] {
	body := BodyValue{decoder: jsontext.NewDecoder(request.Body, jsontext.AllowDuplicateNames(true)), path: bodyRoot}
	single := func(value V) BodyReading[V] {
		return mo.Fold(mo.TupleToResult(body.decoder.ReadToken()), func(jsontext.Token) BodyReading[V] {
			return BodyRefused{Cause: BodyNotSingle{}}
		}, func(cause error) BodyReading[V] {
			if errors.Is(cause, io.EOF) {
				return Read[V]{Value: value}
			}
			return body.unread(cause)
		})
	}
	switch reading := Next(read(body), single).(type) {
	case Read[V]:
		return reading
	case BodyRefused:
		return RequestRefused{Part: RequestRefusedPartBody, Name: bodyCausePath(reading.Cause), Reason: reading.Cause.Error()}
	case nil:
	}
	return RequestRefused{Part: RequestRefusedPartBody, Name: bodyRoot, Reason: "the body was not read"}
}

// BodyText is a JSON string as the text type T its field holds.
func BodyText[T ~string](value BodyValue) BodyReading[T] {
	return Next(value.token("a string", jsontext.KindString), func(read jsontext.Token) BodyReading[T] { return Read[T]{Value: T(read.String())} })
}

// BodyTruth is a JSON true or false as the boolean type B its field holds.
func BodyTruth[B ~bool](value BodyValue) BodyReading[B] {
	return Next(value.token("true or false", jsontext.KindTrue, jsontext.KindFalse), func(read jsontext.Token) BodyReading[B] {
		return Read[B]{Value: B(read.Bool())}
	})
}

// numberRange is the least and the most a number type holds, written as JSON writes them.
type numberRange struct {
	least string
	most  string
}

// signedRange is the least and the most the signed type I holds.
func signedRange[I signedWhole]() numberRange {
	bits := 0
	for shifted := I(1); shifted != 0; shifted <<= 1 {
		bits++
	}
	most := int64(1)<<(bits-1) - 1
	return numberRange{least: strconv.FormatInt(-most-1, 10), most: strconv.FormatInt(most, 10)}
}

// unsignedRange is the least and the most the type U holds.
func unsignedRange[U unsignedWhole]() numberRange {
	return numberRange{least: "0", most: strconv.FormatUint(uint64(^U(0)), 10)}
}

// whole is value's JSON number parsed by parse into its field's type, whose range is within: a
// number parse refuses as out of range is outside within, and any other it refuses is written with
// a fraction or an exponent.
func whole[V any](value BodyValue, within numberRange, parse func(string) mo.Result[V]) BodyReading[V] {
	return Next(value.token("a number", jsontext.KindNumber), func(token jsontext.Token) BodyReading[V] {
		read := token.String()
		return mo.Fold(parse(read), func(fitted V) BodyReading[V] { return Read[V]{Value: fitted} }, func(cause error) BodyReading[V] {
			if errors.Is(cause, strconv.ErrRange) {
				return BodyRefused{Cause: BodyNumberOutside{Path: value.path, Read: read, Least: within.least, Most: within.most}}
			}
			return BodyRefused{Cause: BodyNumberNotWhole{Path: value.path, Read: read}}
		})
	})
}

// BodySigned is a JSON whole number as the signed type I its field holds, refused when I does not
// hold it.
func BodySigned[I signedWhole](value BodyValue) BodyReading[I] {
	return whole(value, signedRange[I](), func(read string) mo.Result[I] {
		return mo.Fold(mo.TupleToResult(strconv.ParseInt(read, 10, 64)), func(parsed int64) mo.Result[I] {
			return mo.Fold(fittedSigned[I](parsed), mo.Ok[I], func(error) mo.Result[I] { return mo.Err[I](strconv.ErrRange) })
		}, mo.Err[I])
	})
}

// BodyUnsigned is a JSON whole number at or above zero as the type U its field holds, refused when U
// does not hold it.
func BodyUnsigned[U unsignedWhole](value BodyValue) BodyReading[U] {
	return whole(value, unsignedRange[U](), func(read string) mo.Result[U] {
		return mo.Fold(mo.TupleToResult(strconv.ParseUint(read, 10, 64)), func(parsed uint64) mo.Result[U] {
			return mo.Fold(fittedUnsigned[U](parsed), mo.Ok[U], func(error) mo.Result[U] { return mo.Err[U](strconv.ErrRange) })
		}, func(unsigned error) mo.Result[U] {
			// ParseUint refuses every negative number as unreadable; a negative whole number is outside
			// U, and -0 is zero.
			return mo.Fold(mo.TupleToResult(strconv.ParseInt(read, 10, 64)), func(signed int64) mo.Result[U] {
				if signed == 0 {
					return mo.Ok[U](0)
				}
				return mo.Err[U](strconv.ErrRange)
			}, func(signed error) mo.Result[U] {
				if errors.Is(signed, strconv.ErrRange) {
					return mo.Err[U](strconv.ErrRange)
				}
				return mo.Err[U](unsigned)
			})
		})
	})
}

// floatBits is the size of the floating-point type F: doubling the largest float32 is infinite only
// in a float32.
func floatBits[F ~float32 | ~float64]() int {
	if math.IsInf(float64(F(math.MaxFloat32)*2), 0) {
		return 32
	}
	return 64
}

// BodyNumber is a JSON number as the floating-point type F its field holds, refused when it is too
// large for F to hold.
func BodyNumber[F ~float32 | ~float64](value BodyValue) BodyReading[F] {
	bits := floatBits[F]()
	most := strconv.FormatFloat(math.MaxFloat64, 'g', -1, 64)
	if bits == 32 {
		most = strconv.FormatFloat(math.MaxFloat32, 'g', -1, 32)
	}
	return Next(value.token("a number", jsontext.KindNumber), func(token jsontext.Token) BodyReading[F] {
		read := token.String()
		// Every JSON number is a number ParseFloat reads, so it refuses one only as too large for bits.
		return mo.Fold(mo.TupleToResult(strconv.ParseFloat(read, bits)), func(parsed float64) BodyReading[F] {
			return Read[F]{Value: F(parsed)}
		}, func(error) BodyReading[F] {
			return BodyRefused{Cause: BodyNumberOutside{Path: value.path, Read: read, Least: "-" + most, Most: most}}
		})
	})
}

// BodyInstant is a JSON string holding an RFC 3339 time, as that instant in UTC.
func BodyInstant(value BodyValue) BodyReading[time.Time] {
	return Next(value.token("a string", jsontext.KindString), func(token jsontext.Token) BodyReading[time.Time] {
		read := token.String()
		return mo.Fold(mo.TupleToResult(time.Parse(time.RFC3339, read)), func(instant time.Time) BodyReading[time.Time] {
			return Read[time.Time]{Value: instant.UTC()}
		}, func(cause error) BodyReading[time.Time] {
			return BodyRefused{Cause: BodyInstantUnreadable{Path: value.path, Read: read, Reason: cause.Error()}}
		})
	})
}

// BodyLiteral reads a JSON string as the string type L its field holds, when it is one of values.
func BodyLiteral[L ~string](values ...L) func(BodyValue) BodyReading[L] {
	return func(value BodyValue) BodyReading[L] {
		return Next(BodyText[L](value), func(read L) BodyReading[L] {
			if slices.Contains(values, read) {
				return Read[L]{Value: read}
			}
			accepted := []string{}
			for _, each := range values {
				accepted = append(accepted, string(each))
			}
			return BodyRefused{Cause: BodyLiteralOutside{Path: value.path, Read: string(read), Values: accepted}}
		})
	}
}

// BodyList reads a JSON array as the slice type L its field holds, each item by item.
func BodyList[L ~[]T, T any](item func(BodyValue) BodyReading[T]) func(BodyValue) BodyReading[L] {
	return func(value BodyValue) BodyReading[L] {
		return Next(value.token("an array", jsontext.KindBeginArray), func(jsontext.Token) BodyReading[L] {
			items := L{}
			for value.decoder.PeekKind() != jsontext.KindEndArray {
				switch read := item(value.item(len(items))).(type) {
				case Read[T]:
					items = append(items, read.Value)
				case BodyRefused:
					return read
				case nil:
					return BodyRefused{Cause: BodyNotReceived{Reason: "an item was not read"}}
				}
			}
			return Next(value.token("the end of an array", jsontext.KindEndArray), func(jsontext.Token) BodyReading[L] { return Read[L]{Value: items} })
		})
	}
}

// BodyArray reads a JSON array of exactly length items, each by item, as the array type A its field
// holds, which fixed converts the items to.
func BodyArray[A, T any](length int, item func(BodyValue) BodyReading[T], fixed func([]T) A) func(BodyValue) BodyReading[A] {
	return func(value BodyValue) BodyReading[A] {
		return Next(BodyList[[]T](item)(value), func(items []T) BodyReading[A] {
			if len(items) != length {
				return BodyRefused{Cause: BodyListLengthWrong{Path: value.path, Length: length, Read: len(items)}}
			}
			return Read[A]{Value: fixed(items)}
		})
	}
}

// BodyTextKey reads the name of an entry of a JSON object as the text type K a dictionary field's
// keys are: every name is a key.
func BodyTextKey[K ~string](member BodyMember) BodyReading[K] {
	return Read[K]{Value: K(member.Name)}
}

// BodyLiteralKey reads the name of an entry of a JSON object as the string type K a dictionary
// field's keys are, when it is one of keys.
func BodyLiteralKey[K ~string](keys ...K) func(BodyMember) BodyReading[K] {
	return func(member BodyMember) BodyReading[K] {
		if slices.Contains(keys, K(member.Name)) {
			return Read[K]{Value: K(member.Name)}
		}
		accepted := []string{}
		for _, each := range keys {
			accepted = append(accepted, string(each))
		}
		return BodyRefused{Cause: BodyKeyOutside{Path: member.Value.path, Read: member.Name, Keys: accepted}}
	}
}

// BodyDictionary reads a JSON object as the map type M its field holds, each entry's name by key
// and its value by entry.
func BodyDictionary[M ~map[K]V, K ~string, V any](key func(BodyMember) BodyReading[K], entry func(BodyValue) BodyReading[V]) func(BodyValue) BodyReading[M] {
	return func(value BodyValue) BodyReading[M] {
		return objectMembers(value, M{}, BodyValue.entry, func(entries M, member BodyMember) BodyReading[M] {
			return Next(key(member), func(read K) BodyReading[M] {
				if _, isHeld := entries[read]; isHeld {
					return BodyRefused{Cause: BodyMemberRepeated{Path: member.Value.path}}
				}
				return Next(entry(member.Value), func(held V) BodyReading[M] {
					entries[read] = held
					return Read[M]{Value: entries}
				})
			})
		})
	}
}

// BodyNullable reads JSON null as no T and any other value as the T inner reads, for a field
// holding a mo.Option.
func BodyNullable[T any](inner func(BodyValue) BodyReading[T]) func(BodyValue) BodyReading[mo.Option[T]] {
	return func(value BodyValue) BodyReading[mo.Option[T]] {
		if value.decoder.PeekKind() == jsontext.KindNull {
			return Next(value.token("null", jsontext.KindNull), func(jsontext.Token) BodyReading[mo.Option[T]] {
				return Read[mo.Option[T]]{Value: mo.None[T]()}
			})
		}
		return Next(inner(value), func(read T) BodyReading[mo.Option[T]] { return Read[mo.Option[T]]{Value: mo.Some(read)} })
	}
}

// BodyMember is one member of a JSON object in a request's body: its name, and its value, which
// is read next.
type BodyMember struct {
	Name  string
	Value BodyValue
}

// objectMembers reads a JSON object as the members M kept from start, each member at the path at
// gives it and read by member into the members read before it.
func objectMembers[M any](value BodyValue, start M, at func(BodyValue, string) BodyValue, member func(M, BodyMember) BodyReading[M]) BodyReading[M] {
	return Next(value.token("an object", jsontext.KindBeginObject), func(jsontext.Token) BodyReading[M] {
		members := start
		for value.decoder.PeekKind() != jsontext.KindEndObject {
			read := Next(value.token("a name", jsontext.KindString), func(name jsontext.Token) BodyReading[M] {
				return member(members, BodyMember{Name: name.String(), Value: at(value, name.String())})
			})
			switch next := read.(type) {
			case Read[M]:
				members = next.Value
			case BodyRefused:
				return next
			case nil:
				return BodyRefused{Cause: BodyNotReceived{Reason: "a member was not read"}}
			}
		}
		return Next(value.token("the end of an object", jsontext.KindEndObject), func(jsontext.Token) BodyReading[M] { return Read[M]{Value: members} })
	})
}

// BodyObject reads a JSON object as the members M the function generated for a struct keeps, from
// start, each member read by member into the members read before it.
func BodyObject[M any](value BodyValue, start M, member func(M, BodyMember) BodyReading[M]) BodyReading[M] {
	return objectMembers(value, start, BodyValue.member, member)
}

// Once is the members kept with member's value, which read reads and kept keeps, or the refusal of a
// member the object already wrote, which held holds.
func Once[V, M any](member BodyMember, held mo.Option[V], read func(BodyValue) BodyReading[V], kept func(V) M) BodyReading[M] {
	return mo.Fold(held, func(V) BodyReading[M] { return BodyRefused{Cause: BodyMemberRepeated{Path: member.Value.path}} }, func(error) BodyReading[M] {
		return Next(read(member.Value), func(value V) BodyReading[M] { return Read[M]{Value: kept(value)} })
	})
}

// Unknown is the refusal of a member the object's struct does not declare.
func Unknown[M any](member BodyMember) BodyReading[M] {
	return BodyRefused{Cause: BodyMemberUnknown{Path: member.Value.path}}
}

// Present is the value of the member name of the object at object, which held holds, or the refusal
// of a member the object does not hold.
func Present[V any](object BodyValue, name string, held mo.Option[V]) BodyReading[V] {
	return mo.Fold(held, func(value V) BodyReading[V] { return Read[V]{Value: value} }, func(error) BodyReading[V] {
		return BodyRefused{Cause: BodyMemberMissing{Path: object.member(name).path}}
	})
}
