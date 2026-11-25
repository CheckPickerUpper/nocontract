package nocontract

import "core:nbio"
import "core:net"

import http "../dependencies/odin-http"
import "supervisor"

@(private)
Announcement_Pending :: struct #all_or_none {}

@(private)
Announcement_Completed :: struct #all_or_none {}

@(private)
Announcement_State :: union #no_nil {
	Announcement_Pending,
	Announcement_Completed,
	supervisor.Readiness_Report_Refusal,
}

@(private)
Listener_Announcement :: struct #all_or_none {
	worker: supervisor.Worker_Entry,
	endpoint: net.Endpoint,
	server: ^http.Server,
	state: Announcement_State,
}

// Listener_Readiness_Not_Announced records a listener that stopped before entering its loop.
Listener_Readiness_Not_Announced :: struct #all_or_none {
	endpoint: net.Endpoint,
}

// announce_listener reports the worker ready to its supervisor once its event loop runs.
@(private)
announce_listener :: proc(_: ^nbio.Operation, announcement: ^Listener_Announcement) {
	switch ready in supervisor.report_ready(announcement.worker, u16(announcement.endpoint.port)) {
	case supervisor.Ready_Reported:
		announcement.state = Announcement_Completed{}
	case supervisor.Readiness_Report_Refusal:
		announcement.state = ready
		http.server_shutdown(announcement.server)
	}
}
