package supervisor

import "core:sys/posix"
import "core:encoding/endian"

// Ready_Reported confirms that the worker published its bound port and restored its mask.
@(not_an_error)
Ready_Reported :: struct #all_or_none {
	port: u16,
}

// Readiness_Report preserves a ready worker or the native failure preventing publication.
Readiness_Report :: union #no_nil {
	Ready_Reported,
	Readiness_Report_Refusal,
}

// report_ready is called after graceful-stop hooks are installed, before serving requests.
// The two-byte port message fits in one atomic pipe write.
@(require_results)
report_ready :: proc(worker: Worker_Entry, port: u16) -> Readiness_Report {
	if port == 0 {
		return Readiness_Report_Refusal(Ready_Port_Zero{worker = worker})
	}
	packet := [size_of(u16)]u8{}
	_ = endian.put_u16(packet[:], .Big, port)
	written := posix.write(worker.readiness, raw_data(packet[:]), size_of(port))
	if written == -1 {
		return Readiness_Report_Refusal(Readiness_Not_Written{worker = worker, port = port, expected = size_of(port), cause = posix.errno()})
	}
	if written != size_of(port) {
		return Readiness_Report_Refusal(Readiness_Write_Short{worker = worker, port = port, expected = size_of(port), written = int(written)})
	}
	if posix.close(worker.readiness) != .OK {
		return Readiness_Report_Refusal(Pipe_Not_Closed{descriptor = worker.readiness, cause = posix.errno()})
	}
	if posix.kill(worker.parent, .SIGUSR1) != .OK {
		return Readiness_Report_Refusal(Signal_Not_Sent{process = worker.parent, signal = .SIGUSR1, cause = posix.errno()})
	}
	original_mask := worker.original_mask
	cause := posix.pthread_sigmask(.SETMASK, &original_mask, nil)
	if cause != .NONE {
		return Readiness_Report_Refusal(Signal_Mask_Not_Changed{operation = .SETMASK, mask = worker.original_mask, cause = cause})
	}
	return Ready_Reported{port = port}
}

@(private)
Readiness_Read :: union #no_nil {
	Worker_Readiness,
	Supervision_Refusal,
}

@(private, require_results)
read_worker_ready :: proc(worker: Live_Worker, expected_port: u16, record: ^$Record, observe: proc(^Record, Supervisor_Event)) -> Readiness_Read {
	switch readiness in worker.readiness {
	case Reported_Port, Readiness_Abandoned:
		return Worker_Readiness(readiness)
	case Awaiting_Readiness:
		packet := [size_of(u16)]u8{}
		received := posix.read(worker.lease.reader, raw_data(packet[:]), size_of(u16))
		if received == -1 {
			cause := posix.errno()
			if cause == .EAGAIN {
				return Worker_Readiness(Awaiting_Readiness{})
			}
			return Supervision_Refusal(Readiness_Not_Read{worker = worker.lease.process, descriptor = worker.lease.reader, cause = cause})
		}
		if received == 0 {
			return Worker_Readiness(Readiness_Abandoned{})
		}
		if received != size_of(u16) {
			return Supervision_Refusal(Readiness_Width_Differed{worker = worker.lease.process, packet = packet, received = int(received), expected = size_of(u16)})
		}
		port, _ := endian.get_u16(packet[:], .Big)
		if port == 0 {
			return Supervision_Refusal(Readiness_Port_Zero{worker = worker.lease.process})
		}
		if port != expected_port {
			return Supervision_Refusal(Readiness_Port_Differed{worker = worker.lease.process, expected = expected_port, reported = port})
		}
		observe(record, Worker_Event{process = worker.lease.process, lifecycle = Worker_Ready{port = port}})
		return Worker_Readiness(Reported_Port{port = port})
	}
	unreachable()
}
