package reading

import "github.com/samber/mo"

// mapOption is option's value passed through change, which mo's same-type Map cannot do.
func mapOption[From, To any](option mo.Option[From], change func(From) To) mo.Option[To] {
	return mo.Fold(option, func(value From) mo.Option[To] { return mo.Some(change(value)) }, func(error) mo.Option[To] { return mo.None[To]() })
}

// thenOption is the option next reads from option's value, absent when option is.
func thenOption[From, To any](option mo.Option[From], next func(From) mo.Option[To]) mo.Option[To] {
	return mo.Fold(option, next, func(error) mo.Option[To] { return mo.None[To]() })
}
