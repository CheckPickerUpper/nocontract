package signin

// check reads a normalized credential header and returns the application's typed guard decision.
// Missing or empty credentials call missing; nonempty credentials call verify once with their
// text unchanged. Both callbacks receive the application's scope.
@(require_results)
check :: proc(
	headers: map[string]string,
	header: string,
	scope: $Scope,
	missing: proc(Scope) -> $Decision,
	verify: proc(Scope, string) -> Decision,
) -> Decision {
	credential, present := headers[header]
	if !present || credential == "" {
		return missing(scope)
	}
	return verify(scope, credential)
}
