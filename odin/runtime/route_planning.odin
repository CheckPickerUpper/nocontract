package nocontract

import requestlayers "layers"

// Selected_Route is the identified generated callback and the request values its matcher read.
Selected_Route :: struct #all_or_none {
	template:   string,
	parameters: map[string]string,
	received:   Received_Request,
	answer:     proc(request: Request, received: Received_Request, parameters: map[string]string) -> Answer,
}

// Immediate_Answer is a typed routing refusal or OPTIONS answer needing no route call.
Immediate_Answer :: struct #all_or_none {
	answer: Answer,
}

// Route_Selection keeps a selected route separate from an immediate routing answer.
Route_Selection :: union #no_nil {
	Selected_Route,
	Immediate_Answer,
}

// Route_Plan is the method read by the matcher and its complete routing decision.
Route_Plan :: struct #all_or_none {
	method:    string,
	selection: Route_Selection,
}

@(private, require_results)
visit_request :: proc(plan: Route_Plan, path: string) -> requestlayers.Visit {
	switch selection in plan.selection {
	case Selected_Route:
		return requestlayers.Visit{method = plan.method, path = path, route = selection.template}
	case Immediate_Answer:
		return requestlayers.Visit{method = plan.method, path = path, route = path}
	}
	unreachable()
}

@(private, require_results)
answer_planned_route :: proc(plan: Route_Plan, request: Request) -> Answer {
	switch selection in plan.selection {
	case Selected_Route:
		return selection.answer(request, selection.received, selection.parameters)
	case Immediate_Answer:
		return selection.answer
	}
	unreachable()
}
