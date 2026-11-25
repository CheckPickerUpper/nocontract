package main

import (
	"os"
	"strconv"

	"github.com/CheckPickerUpper/outcome"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

// withTestRequestDeadline applies the deadline SHOP_TEST_REQUEST_DEADLINE names in milliseconds,
// and leaves the shop without a deadline when it is unset.
func withTestRequestDeadline(options nocontract.ListenOptions) outcome.Result[nocontract.ListenOptions] {
	value, set := os.LookupEnv("SHOP_TEST_REQUEST_DEADLINE")
	if !set {
		return outcome.Ok(options)
	}
	return outcome.Fold(outcome.TupleToResult(strconv.ParseUint(value, 10, 32)), func(milliseconds uint64) outcome.Result[nocontract.ListenOptions] {
		return nocontract.WithRequestDeadline(options, uint32(milliseconds))
	}, outcome.Err[nocontract.ListenOptions])
}
