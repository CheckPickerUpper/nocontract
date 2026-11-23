package shopperauth

import "nocontract:odin/runtime/signin"

// Admitted is a request with a verified shopper credential.
@(not_an_error)
Admitted :: struct #all_or_none {}

// NotSignedIn is a request carrying no shopper.
//nocontract:status 401
@(error_code = "SH-1101")
NotSignedIn :: struct #all_or_none {
	scope: string,
}

// ShopperCredentialRejected is a request with an unrecognized shopper credential.
//nocontract:status 401
@(error_code = "SH-1102")
ShopperCredentialRejected :: struct #all_or_none {
	scope: string,
}

// CheckOutcome is the shopper check's admission or refusal.
CheckOutcome :: union #no_nil {
	Admitted,
	NotSignedIn,
	ShopperCredentialRejected,
}

// Check admits the example credential shopper-7 and refuses missing or unrecognized credentials.
@(require_results)
Check :: proc(scope: string, headers: map[string]string) -> CheckOutcome {
	return signin.check(
		headers,
		"x-shopper",
		scope,
		proc(scope: string) -> CheckOutcome {
			return NotSignedIn{scope = scope}
		},
		proc(scope: string, credential: string) -> CheckOutcome {
			if credential == "shopper-7" {
				return Admitted{}
			}
			return ShopperCredentialRejected{scope = scope}
		},
	)
}
