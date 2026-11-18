package reading

import (
	"slices"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
)

// found is what a reading step found beside its value, each in the order found: the declarations
// it refused, the members and named structs it read, and the packages whose declarations it read.
type found struct {
	refusals []assembling.Refusal
	members  []foundMember
	structs  []foundStruct
	packages []string
}

// foundMember is a member read whole, with the declaration it was read from.
type foundMember struct {
	key      typeKey
	position assembling.Position
	member   description.Member
}

// foundStruct is a named struct read whole, with the declaration it was read from.
type foundStruct struct {
	key      typeKey
	position assembling.Position
	named    description.NamedStruct
}

// nothingFound is a step that found nothing beside its value.
func nothingFound() found {
	return found{refusals: []assembling.Refusal{}, members: []foundMember{}, structs: []foundStruct{}, packages: []string{}}
}

// with is everything found and everything other found, in that order.
func (finding found) with(other found) found {
	return found{
		refusals: slices.Concat(finding.refusals, other.refusals),
		members:  slices.Concat(finding.members, other.members),
		structs:  slices.Concat(finding.structs, other.structs),
		packages: slices.Concat(finding.packages, other.packages),
	}
}

// refusedAt is a step that refused one declaration.
func refusedAt(position assembling.Position, problem assembling.Problem) found {
	return found{refusals: []assembling.Refusal{{Position: position, Problem: problem}}, members: []foundMember{}, structs: []foundStruct{}, packages: []string{}}
}

// packageRead is a step that read one package's declarations.
func packageRead(path string) found {
	return found{refusals: []assembling.Refusal{}, members: []foundMember{}, structs: []foundStruct{}, packages: []string{path}}
}

// memberRead is a step that read one member whole.
func memberRead(member foundMember) found {
	return found{refusals: []assembling.Refusal{}, members: []foundMember{member}, structs: []foundStruct{}, packages: []string{}}
}

// structRead is a step that read one named struct whole.
func structRead(named foundStruct) found {
	return found{refusals: []assembling.Refusal{}, members: []foundMember{}, structs: []foundStruct{named}, packages: []string{}}
}

// reading is a step's value, absent when the step refused what it read, and what it found.
type reading[T any] struct {
	value mo.Option[T]
	found found
}

// readAs is a value read, with what reading it found.
func readAs[T any](value T, finding found) reading[T] {
	return reading[T]{value: mo.Some(value), found: finding}
}

// notRead is a step that has no value, with what it found, such as why it refused.
func notRead[T any](finding found) reading[T] {
	return reading[T]{value: mo.None[T](), found: finding}
}

// andThen is next's reading of read's value, with both steps' findings; nothing when read has no
// value.
func andThen[From, To any](read reading[From], next func(From) reading[To]) reading[To] {
	return mo.Fold(read.value, func(value From) reading[To] {
		following := next(value)
		return reading[To]{value: following.value, found: read.found.with(following.found)}
	}, func(error) reading[To] { return notRead[To](read.found) })
}

// mapped is read's value passed through change.
func mapped[From, To any](read reading[From], change func(From) To) reading[To] {
	return reading[To]{value: mapOption(read.value, change), found: read.found}
}

// pair is two values read independently of each other.
type pair[First, Second any] struct {
	first  First
	second Second
}

// paired is two readings that do not depend on each other: both values when both are read, and
// everything either found whether or not the other was read.
func paired[First, Second any](first reading[First], second reading[Second]) reading[pair[First, Second]] {
	return reading[pair[First, Second]]{
		value: thenOption(first.value, func(read First) mo.Option[pair[First, Second]] {
			return mapOption(second.value, func(other Second) pair[First, Second] { return pair[First, Second]{first: read, second: other} })
		}),
		found: first.found.with(second.found),
	}
}

// alongside is read with more found beside it.
func alongside[T any](read reading[T], more found) reading[T] {
	return reading[T]{value: read.value, found: read.found.with(more)}
}

// collected reads each item with read: every value when every item is read whole, nothing when one
// is not, and everything all of them found either way.
func collected[Item, T any](items []Item, read func(Item) reading[T]) reading[[]T] {
	gathered := readAs([]T{}, nothingFound())
	for _, item := range items {
		each := read(item)
		gathered = reading[[]T]{
			value: thenOption(gathered.value, func(values []T) mo.Option[[]T] {
				return mapOption(each.value, func(value T) []T { return append(slices.Clone(values), value) })
			}),
			found: gathered.found.with(each.found),
		}
	}
	return gathered
}
