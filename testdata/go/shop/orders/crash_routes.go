package orders

import "context"

// CrashInput selects one deliberate crash used to verify the shop server's recovery.
type CrashInput struct {
	Cause string `path:"cause"`
}

// Crash demonstrates a panic or a bounds failure at the shop's own request boundary.
//
//nocontract:route GET /crashes/{cause}
func Crash(_ context.Context, input CrashInput) OrderCountOutcome {
	switch input.Cause {
	case "panic":
		panic("deliberate shop panic")
	case "bounds":
		counts := []int64{}
		return OrderCountRead{OrderCount: counts[len(counts)]}
	}
	return OrderCountRead{OrderCount: 7}
}
