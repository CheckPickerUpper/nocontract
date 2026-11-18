// Package keyed reads values by key without a yes or no beside them: a map's entry as an option,
// the first item of each key, and each item whose key an earlier item already took.
package keyed

import "github.com/CheckPickerUpper/outcome"

// Entry is the value the map holds for key, when it holds one.
func Entry[Key comparable, Value any](values map[Key]Value, key Key) outcome.Option[Value] {
	value, held := values[key]
	return outcome.TupleToOption(value, held)
}

// FirstOfEach is every item whose key no earlier item took, in order.
func FirstOfEach[Item any, Key comparable](items []Item, key func(Item) Key) []Item {
	kept, taken := []Item{}, map[Key]struct{}{}
	for _, item := range items {
		outcome.FoldOption(Entry(taken, key(item)), func(struct{}) struct{} { return struct{}{} }, func() struct{} {
			taken[key(item)] = struct{}{}
			kept = append(kept, item)
			return struct{}{}
		})
	}
	return kept
}

// Repeat is an item whose key an earlier item took, and that earlier item.
type Repeat[Item any] struct {
	Item  Item
	First Item
}

// Repeats is every item whose key an earlier item took, each with the first item of its key, in
// order.
func Repeats[Item any, Key comparable](items []Item, key func(Item) Key) []Repeat[Item] {
	repeats, first := []Repeat[Item]{}, map[Key]Item{}
	for _, item := range items {
		outcome.FoldOption(Entry(first, key(item)), func(taken Item) struct{} {
			repeats = append(repeats, Repeat[Item]{Item: item, First: taken})
			return struct{}{}
		}, func() struct{} {
			first[key(item)] = item
			return struct{}{}
		})
	}
	return repeats
}
