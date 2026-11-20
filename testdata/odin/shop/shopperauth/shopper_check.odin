package shopperauth

// Admitted is a request naming a shopper.
@(not_an_error)
Admitted :: struct #all_or_none {}

// NotSignedIn is a request carrying no shopper.
//nocontract:status 401
@(error_code = "SH-1101")
NotSignedIn :: struct #all_or_none {
	scope: string,
}

// CheckOutcome is the shopper check's admission or refusal.
CheckOutcome :: union #no_nil {
	Admitted,
	NotSignedIn,
}

// Check admits a request naming a shopper.
@(require_results)
Check :: proc(scope: string, headers: map[string]string) -> CheckOutcome {
	shopper, present := headers["x-shopper"]
	if !present || shopper == "" {
		return NotSignedIn{scope = scope}
	}
	return Admitted{}
}
