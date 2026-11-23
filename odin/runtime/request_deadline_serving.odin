package nocontract

import "core:log"
import "core:mem"
import "core:nbio"
import "core:strings"
import "core:sys/linux"
import "core:thread"
import "core:time"

import http "../dependencies/odin-http"
import "httpbinding"
import requestlayers "layers"
import "requestlifetime"

@(private)
Unscheduled_Response :: struct #all_or_none {
	response: ^http.Response,
	allocator: mem.Allocator,
}

@(private)
Awaiting_Response :: struct #all_or_none {
	response: ^http.Response,
	allocator: mem.Allocator,
	timeout: ^nbio.Operation,
}

@(private)
Deadline_Checked_Response :: struct #all_or_none {
	response: ^http.Response,
	allocator: mem.Allocator,
}

@(private)
Released_Response :: struct #all_or_none {}

@(private)
Response_Ownership :: union #no_nil {
	Unscheduled_Response,
	Awaiting_Response,
	Deadline_Checked_Response,
	Released_Response,
}

@(private)
No_Request_Worker :: struct #all_or_none {}

@(private)
Running_Worker :: struct #all_or_none { value: ^thread.Thread }

@(private)
Worker_Ownership :: union #no_nil { No_Request_Worker, Running_Worker }

@(private)
Read_Not_Started :: struct #all_or_none {}

@(private)
Pending_Read :: struct #all_or_none { operation: ^nbio.Operation }

@(private)
Finished_Read :: struct #all_or_none {}

@(private)
Completion_Read :: union #no_nil { Read_Not_Started, Pending_Read, Finished_Read }

@(private)
Eventfd_Notice :: struct #all_or_none {}

@(private)
Failed_Notice :: struct #all_or_none { errno: linux.Errno }

@(private)
Completion_Notice :: union #no_nil { Eventfd_Notice, Failed_Notice }

@(private)
Uncomputed_Answer :: struct #all_or_none {}

@(private)
Computed_Answer :: struct #all_or_none { value: Answer }

@(private)
Worker_Answer :: union #no_nil { Uncomputed_Answer, Computed_Answer }

// The I/O thread owns response, reading and worker. The worker owns answer and notice.
// Joining the worker precedes reading those fields or reclaiming any arena storage.
@(private)
Deadline_Request :: struct #all_or_none {
	binding: Binding,
	selected: Selected_Route,
	request: Request,
	visit: requestlayers.Visit,
	layers: []requestlayers.Layer,
	started: time.Tick,
	lifetime: requestlifetime.Request_Lifetime,
	arena: mem.Dynamic_Arena,
	allocator: mem.Allocator,
	loop: ^nbio.Event_Loop,
	descriptor: linux.Fd,
	read_buffer: [8]u8,
	response: Response_Ownership,
	reading: Completion_Read,
	worker: Worker_Ownership,
	answer: Worker_Answer,
	notice: Completion_Notice,
}

@(private)
start_deadline_request :: proc(api: ^Serving_API, response: ^http.Response, request: Request, selected: Selected_Route, visit: requestlayers.Visit, deadline: Configured_Deadline, started: time.Tick) {
	descriptor, cause := linux.eventfd(0, {.CLOEXEC, .NONBLOCK})
	if i32(cause) != 0 {
		context.allocator = context.temp_allocator
		publish_answer(response, visit, api.binding.completion_unavailable(selected.template, "eventfd", i32(cause)), started)
		return
	}
	duration := time.Duration(deadline.milliseconds)*time.Millisecond
	work := new_clone(Deadline_Request{
		binding = api.binding,
		selected = selected,
		request = request,
		visit = visit,
		layers = api.layers,
		started = started,
		lifetime = requestlifetime.Request_Lifetime{deadline = time.tick_add(time.tick_now(), duration), duration = duration, completion = .Pending},
		arena = {},
		allocator = api.allocator,
		loop = nbio.current_thread_event_loop(),
		descriptor = descriptor,
		read_buffer = {},
		response = Unscheduled_Response{response = response, allocator = context.temp_allocator},
		reading = Read_Not_Started{},
		worker = No_Request_Worker{},
		answer = Uncomputed_Answer{},
		notice = Eventfd_Notice{},
	}, api.allocator)
	mem.dynamic_arena_init(&work.arena, block_allocator = api.allocator, array_allocator = api.allocator)
	private_allocator := mem.dynamic_arena_allocator(&work.arena)
	work.selected = copy_selected_request(selected, private_allocator)
	work.request = copy_deadline_request(request, work.selected.received, &work.lifetime, private_allocator)
	work.visit = requestlayers.Visit{method = strings.clone(visit.method, private_allocator), path = work.request.path, route = work.selected.template}
	// This registered read keeps the I/O loop alive even after a timeout sends and frees the HTTP request.
	work.reading = Pending_Read{operation = nbio.read_poly(nbio.Handle(descriptor), 0, work.read_buffer[:], work, receive_request_completion)}
	remaining := max(time.Duration(0), time.tick_diff(time.tick_now(), work.lifetime.deadline))
	timeout := nbio.timeout_poly(remaining, work, expire_request)
	work.response = Awaiting_Response{response = response, allocator = context.temp_allocator, timeout = timeout}
	worker_context := context
	worker_context.allocator = mem.dynamic_arena_allocator(&work.arena)
	worker_context.temp_allocator = worker_context.allocator
	context.allocator = work.allocator
	work.worker = Running_Worker{value = thread.create_and_start_with_poly_data(work, invoke_request_handler, init_context = worker_context)}
}

@(private, require_results)
copy_selected_request :: proc(selected: Selected_Route, allocator: mem.Allocator) -> Selected_Route {
	context.allocator = allocator
	context.temp_allocator = allocator
	parameters := make(map[string]string)
	for key, value in selected.parameters { parameters[strings.clone(key)] = strings.clone(value) }
	reading := selected.received.body.reading
	switch source in selected.received.body.reading {
	case httpbinding.Received_Body:
	case httpbinding.Body_Not_Received:
		reading = httpbinding.Body_Not_Received{path = strings.clone(source.path), cause = source.cause}
	}
	return Selected_Route{
		template = strings.clone(selected.template),
		received = Received_Request{method = strings.clone(selected.received.method), body = httpbinding.Body_Receipt{text = strings.clone(selected.received.body.text), reading = reading}},
		parameters = parameters,
		answer = selected.answer,
	}
}

@(private, require_results)
copy_deadline_request :: proc(request: Request, received: Received_Request, lifetime: ^requestlifetime.Request_Lifetime, allocator: mem.Allocator) -> Request {
	context.allocator = allocator
	context.temp_allocator = allocator
	headers := make(map[string]string)
	for key, value in request.headers { headers[strings.clone(key)] = strings.clone(value) }
	return Request{
		path = strings.clone(request.path),
		query = strings.clone(request.query),
		headers = headers,
		content = received,
		lifetime = requestlifetime.Deadline_Lifetime{value = lifetime},
	}
}

// The application handler executes on this worker thread with its own arena.
@(private)
invoke_request_handler :: proc(work: ^Deadline_Request) {
	saved_context := context
	defer { context = saved_context }
	context.allocator = mem.dynamic_arena_allocator(&work.arena)
	context.temp_allocator = context.allocator
	selected := work.selected
	plan := Route_Plan{method = work.visit.method, selection = selected}
	work.answer = Computed_Answer{value = answer_through_layers(work.layers, work.visit, plan, work.request)}
	snapshot := requestlifetime.read_request_lifetime(&work.lifetime)
	if time.tick_diff(time.tick_now(), snapshot.deadline) <= 0 {
		_ = requestlifetime.claim_deadline(&work.lifetime)
	} else {
		_ = requestlifetime.claim_answer(&work.lifetime)
	}
	count := [1]u64{1}
	_, cause := linux.write(work.descriptor, mem.slice_to_bytes(count[:]))
	if i32(cause) != 0 {
		work.notice = Failed_Notice{errno = cause}
		// Failure is delivered on the owning I/O loop, with the original errno.
		nbio.timeout_poly(0, work, receive_failed_notice, l = work.loop)
	}
}

@(private)
expire_request :: proc(_: ^nbio.Operation, work: ^Deadline_Request) {
	claimed := requestlifetime.claim_deadline(&work.lifetime)
	switch response in work.response {
	case Awaiting_Response:
		switch claimed.winner {
		case .Deadline_Claimed:
			work.response = Released_Response{}
			context.allocator = response.allocator
			context.temp_allocator = response.allocator
			publish_answer(response.response, work.visit, work.binding.deadline_exceeded(work.selected.template, u32(work.lifetime.duration/time.Millisecond)), work.started)
		case .Answer_Claimed, .Crash_Claimed:
			work.response = Deadline_Checked_Response{response = response.response, allocator = response.allocator}
		case .Pending:
		}
	case Unscheduled_Response, Deadline_Checked_Response, Released_Response:
	}
}

@(private)
join_request_worker :: proc(work: ^Deadline_Request) {
	switch worker in work.worker {
	case Running_Worker:
		thread.join(worker.value)
		thread.destroy(worker.value)
		work.worker = No_Request_Worker{}
	case No_Request_Worker:
	}
}

@(private)
receive_request_completion :: proc(operation: ^nbio.Operation, work: ^Deadline_Request) {
	work.reading = Finished_Read{}
	join_request_worker(work)
	if i32(operation.read.err) != 0 {
		finish_completion_failure(work, "eventfd-read", i32(operation.read.err))
	} else {
		finish_computed_answer(work)
	}
	switch _ in work.notice {
	case Eventfd_Notice:
		destroy_deadline_request(work)
	case Failed_Notice:
		// The queued failure callback still owns the storage until it runs.
	}
}

@(private)
receive_failed_notice :: proc(_: ^nbio.Operation, work: ^Deadline_Request) {
	join_request_worker(work)
	switch reading in work.reading {
	case Pending_Read:
		nbio.remove(reading.operation)
		work.reading = Finished_Read{}
	case Read_Not_Started, Finished_Read:
	}
	switch notice in work.notice {
	case Failed_Notice:
		finish_completion_failure(work, "eventfd-write", i32(notice.errno))
	case Eventfd_Notice:
	}
	destroy_deadline_request(work)
}

@(private)
finish_computed_answer :: proc(work: ^Deadline_Request) {
	switch response in work.response {
	case Awaiting_Response:
		nbio.remove(response.timeout)
		work.response = Released_Response{}
		write_computed_answer(work, response.response, response.allocator)
	case Deadline_Checked_Response:
		work.response = Released_Response{}
		write_computed_answer(work, response.response, response.allocator)
	case Unscheduled_Response, Released_Response:
	}
}

@(private)
write_computed_answer :: proc(work: ^Deadline_Request, response: ^http.Response, allocator: mem.Allocator) {
	context.allocator = allocator
	context.temp_allocator = allocator
	switch requestlifetime.read_request_lifetime(&work.lifetime).completion {
	case .Deadline_Claimed:
		publish_answer(response, work.visit, work.binding.deadline_exceeded(work.selected.template, u32(work.lifetime.duration/time.Millisecond)), work.started)
	case .Answer_Claimed:
		switch answer in work.answer {
		case Computed_Answer:
			// odin-http copies JSON into its own response buffer before returning.
			publish_answer(response, work.visit, answer.value, work.started)
		case Uncomputed_Answer:
		}
	case .Pending, .Crash_Claimed:
	}
}

@(private)
finish_completion_failure :: proc(work: ^Deadline_Request, operation: string, errno: i32) {
	switch response in work.response {
	case Awaiting_Response:
		nbio.remove(response.timeout)
		work.response = Released_Response{}
		context.allocator = response.allocator
		context.temp_allocator = response.allocator
		publish_answer(response.response, work.visit, work.binding.completion_unavailable(work.selected.template, operation, errno), work.started)
	case Deadline_Checked_Response:
		work.response = Released_Response{}
		context.allocator = response.allocator
		context.temp_allocator = response.allocator
		publish_answer(response.response, work.visit, work.binding.completion_unavailable(work.selected.template, operation, errno), work.started)
	case Unscheduled_Response, Released_Response:
	}
}

@(private)
destroy_deadline_request :: proc(work: ^Deadline_Request) {
	cause := linux.close(work.descriptor)
	if i32(cause) != 0 { log.errorf("NoContract could not close request completion eventfd: %v", cause) }
	mem.dynamic_arena_destroy(&work.arena)
	free(work, work.allocator)
}
