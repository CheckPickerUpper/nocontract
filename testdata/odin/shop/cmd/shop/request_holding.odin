#+private
package main

import "core:net"
import "core:os"
import "core:sync"
import "core:sys/posix"
import "core:thread"
import "core:time"

No_Request_Hold :: struct #all_or_none {}
Request_Released :: struct #all_or_none {}

Request_Hold_Address_Invalid :: struct #all_or_none {
	address: string,
}

Request_Hold_Failed :: struct #all_or_none {
	address: string,
	cause:   net.Network_Error,
}

Request_Hold_Exchange_Incomplete :: struct #all_or_none {
	address:  string,
	received: int,
	sent:     int,
}

Request_Hold_Signals_Not_Blocked :: struct #all_or_none {
	address: string,
	cause:   posix.Errno,
}

Request_Hold :: union #no_nil {
	No_Request_Hold,
	Request_Released,
	Request_Hold_Address_Invalid,
	Request_Hold_Failed,
	Request_Hold_Exchange_Incomplete,
	Request_Hold_Signals_Not_Blocked,
}

Held_Request :: struct #all_or_none {
	address:  string,
	endpoint: net.Endpoint,
	finished: sync.One_Shot_Event,
	answer:   Request_Hold,
}

// The opt-in control connection proves application entry and echoes the release byte.
@(require_results)
hold_shop_request :: proc() -> Request_Hold {
	address := os.get_env("SHOP_HOLD_REQUEST_ADDRESS", context.temp_allocator)
	if address == "" {
		return No_Request_Hold{}
	}
	endpoint, valid := net.parse_endpoint(address)
	if !valid || !net.is_loopback(endpoint.address) {
		return Request_Hold_Address_Invalid{address = address}
	}
	held := Held_Request{address = address, endpoint = endpoint, finished = {}, answer = No_Request_Hold{}}
	waiter := thread.create_and_start_with_poly_data(&held, receive_shop_request_release, context)
	defer thread.destroy(waiter)
	sync.one_shot_event_wait(&held.finished)
	thread.join(waiter)
	return held.answer
}

receive_shop_request_release :: proc(held: ^Held_Request) {
	defer sync.one_shot_event_signal(&held.finished)
	// Only the control socket's helper blocks shutdown signals. The serving thread
	// stays eligible to stop the listener while it waits on the one-shot event.
	signals := posix.sigset_t{}
	if posix.sigemptyset(&signals) != 0 {
		held.answer = Request_Hold_Signals_Not_Blocked{address = held.address, cause = posix.errno()}
		return
	}
	interrupts := []posix.Signal{.SIGINT, .SIGTERM}
	for interrupt in interrupts {
		switch posix.sigaddset(&signals, interrupt) {
		case .OK:
		case .FAIL:
			held.answer = Request_Hold_Signals_Not_Blocked{address = held.address, cause = posix.errno()}
			return
		}
	}
	previous := posix.sigset_t{}
	if mask_error := posix.pthread_sigmask(.BLOCK, &signals, &previous); mask_error != .NONE {
		held.answer = Request_Hold_Signals_Not_Blocked{address = held.address, cause = mask_error}
		return
	}
	held.answer = exchange_shop_request_release(held.address, held.endpoint)
}

@(require_results)
exchange_shop_request_release :: proc(address: string, endpoint: net.Endpoint) -> Request_Hold {
	socket, dial_error := net.dial_tcp(endpoint)
	switch cause in dial_error {
	case nil:
	case net.Create_Socket_Error, net.Dial_Error, net.Listen_Error, net.Accept_Error, net.Bind_Error,
		net.TCP_Send_Error, net.UDP_Send_Error, net.TCP_Recv_Error, net.UDP_Recv_Error, net.Shutdown_Error,
		net.Interfaces_Error, net.Socket_Info_Error, net.Socket_Option_Error, net.Set_Blocking_Error,
		net.Parse_Endpoint_Error, net.Resolve_Error, net.DNS_Error:
		return Request_Hold_Failed{address = address, cause = cause}
	}
	defer net.close(socket)
	socket_options := []net.Socket_Option{.Receive_Timeout, .Send_Timeout}
	for option in socket_options {
		switch option_error := net.set_option(socket, option, 5 * time.Second); option_error {
		case .None:
		case .Network_Unreachable, .Insufficient_Resources, .Invalid_Socket, .Invalid_Option, .Invalid_Value, .Unknown:
			return Request_Hold_Failed{address = address, cause = option_error}
		}
	}
	release := [1]u8{}
	received, receive_error := net.recv_tcp(socket, release[:])
	switch receive_error {
	case .None:
	case .Network_Unreachable, .Insufficient_Resources, .Invalid_Argument, .Not_Connected,
		.Connection_Closed, .Timeout, .Would_Block, .Interrupted, .Unknown:
		return Request_Hold_Failed{address = address, cause = receive_error}
	}
	if received != len(release) {
		return Request_Hold_Exchange_Incomplete{address = address, received = received, sent = 0}
	}
	sent, send_error := net.send_tcp(socket, release[:])
	switch send_error {
	case .None:
	case .Network_Unreachable, .Insufficient_Resources, .Invalid_Argument, .Not_Connected,
		.Connection_Closed, .Host_Unreachable, .Timeout, .Would_Block, .Interrupted, .Unknown:
		return Request_Hold_Failed{address = address, cause = send_error}
	}
	if sent != len(release) {
		return Request_Hold_Exchange_Incomplete{address = address, received = received, sent = sent}
	}
	return Request_Released{}
}
