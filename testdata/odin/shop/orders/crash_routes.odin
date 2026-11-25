package orders

// CrashInput selects a deliberate crash in the shop fixture.
CrashInput :: struct #all_or_none {
	cause: string,
}

// Crash demonstrates request recovery using the shop's own deliberate errors.
//nocontract:route GET /crashes/{cause}
@(require_results)
Crash :: proc(input: CrashInput) -> OrderCountOutcome {
	switch input.cause {
	case "panic":
		panic("deliberate shop panic")
	case "bounds":
		counts := []i64{}
		return OrderCountRead{order_count = counts[len(input.cause)]}
	}
	return OrderCountRead{order_count = 7}
}
