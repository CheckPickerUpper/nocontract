package httpbinding

import "core:sys/posix"

import http "../../dependencies/odin-http"

// Shutdown_Handlers_Installed confirms the library's graceful handler owns both stop signals.
Shutdown_Handlers_Installed :: struct #all_or_none {}

// Shutdown_Handler_Not_Installed preserves the exact signal operation and native failure.
Shutdown_Handler_Not_Installed :: struct #all_or_none {
	signal: posix.Signal,
	cause: posix.Errno,
}

// Shutdown_Installation records the installed signal handlers or the refused operation.
Shutdown_Installation :: union #no_nil {
	Shutdown_Handlers_Installed,
	Shutdown_Handler_Not_Installed,
}

// install_shutdown_handlers forwards SIGINT and SIGTERM through odin-http's graceful owner.
@(require_results)
install_shutdown_handlers :: proc(server: ^http.Server) -> Shutdown_Installation {
	http.server_shutdown_on_interrupt(server)
	action := posix.sigaction_t{}
	switch posix.sigaction(.SIGINT, nil, &action) {
	case .OK:
	case .FAIL:
		return Shutdown_Handler_Not_Installed{signal = .SIGINT, cause = posix.errno()}
	}
	switch posix.sigaction(.SIGTERM, &action, nil) {
	case .OK:
		return Shutdown_Handlers_Installed{}
	case .FAIL:
		return Shutdown_Handler_Not_Installed{signal = .SIGTERM, cause = posix.errno()}
	}
	unreachable()
}
