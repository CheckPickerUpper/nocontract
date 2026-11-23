#+test
package signin

import "core:crypto"
import "core:slice"
import "core:testing"

@(private="file")
Admitted :: struct #all_or_none {}

@(private="file")
Credential_Missing :: struct #all_or_none {
	header: string,
}

@(private="file")
Credential_Rejected :: struct #all_or_none {
	scope: string,
}

@(private="file")
Sign_In_Decision :: union #no_nil {
	Admitted,
	Credential_Missing,
	Credential_Rejected,
}

@(private="file")
Sign_In_Application :: struct #all_or_none {
	scope:          string,
	missing_calls:  int,
	verified_calls: int,
}

@(private="file", require_results)
missing_credential :: proc(application: ^Sign_In_Application) -> Sign_In_Decision {
	application^ = Sign_In_Application{
		scope          = application.scope,
		missing_calls  = application.missing_calls + 1,
		verified_calls = application.verified_calls,
	}
	return Credential_Missing{header = "x-session"}
}

@(private="file", require_results)
verify_credential :: proc(application: ^Sign_In_Application, credential: string) -> Sign_In_Decision {
	application^ = Sign_In_Application{
		scope          = application.scope,
		missing_calls  = application.missing_calls,
		verified_calls = application.verified_calls + 1,
	}
	accepted := "shopper-7"
	if crypto.compare_constant_time(
		slice.from_ptr(raw_data(credential), len(credential)),
		slice.from_ptr(raw_data(accepted), len(accepted)),
	) == 1 {
		return Admitted{}
	}
	return Credential_Rejected{scope = application.scope}
}

@(test)
sign_in_uses_the_chosen_header_and_application_verifier :: proc(t: ^testing.T) {
	cases := []struct #all_or_none {
		header:         string,
		credential:     string,
		expected:       Sign_In_Decision,
		missing_calls:  int,
		verified_calls: int,
	}{
		{
			header         = "x-shopper",
			credential     = "shopper-7",
			expected       = Credential_Missing{header = "x-session"},
			missing_calls  = 1,
			verified_calls = 0,
		},
		{
			header         = "x-session",
			credential     = "",
			expected       = Credential_Missing{header = "x-session"},
			missing_calls  = 1,
			verified_calls = 0,
		},
		{
			header         = "x-session",
			credential     = "shopper-7",
			expected       = Admitted{},
			missing_calls  = 0,
			verified_calls = 1,
		},
		{
			header         = "x-session",
			credential     = "shopper-8",
			expected       = Credential_Rejected{scope = "read-balance"},
			missing_calls  = 0,
			verified_calls = 1,
		},
		{
			header         = "x-session",
			credential     = " shopper-7 ",
			expected       = Credential_Rejected{scope = "read-balance"},
			missing_calls  = 0,
			verified_calls = 1,
		},
	}
	headers := make(map[string]string)
	defer delete(headers)
	for each in cases {
		clear(&headers)
		headers[each.header] = each.credential
		application := Sign_In_Application{
			scope = "read-balance",
			missing_calls = 0,
			verified_calls = 0,
		}
		decision := check(headers, "x-session", &application, missing_credential, verify_credential)
		testing.expect_value(t, decision, each.expected)
		testing.expect_value(t, application.missing_calls, each.missing_calls)
		testing.expect_value(t, application.verified_calls, each.verified_calls)
	}
}
