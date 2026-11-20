package odin

const bindingRuntime = `
@(private)
Wire_Refused :: struct #all_or_none { reason: string }
@(private)
Encoded :: struct #all_or_none { line: string }
@(private)
Reply :: union #no_nil { Encoded, Wire_Refused }

@(private, require_results)
json_reply :: proc(value: $T, location := #caller_location) -> Reply {
	encoded, failure := json.marshal(value, {spec = .JSON})
	switch cause in failure {
	case nil:
		return Encoded{line = string(encoded)}
	case json.Marshal_Data_Error, io.Error:
		return Wire_Refused{reason = fmt.tprintf("encoding %v at %v: %v", type_info_of(T), location, cause)}
	}
	unreachable()
}

main :: proc() {
	reader := bufio.Reader{}
	bufio.reader_init(&reader, os.to_stream(os.stdin))
	defer bufio.reader_destroy(&reader)
	arena := mem.Dynamic_Arena{}
	mem.dynamic_arena_init(&arena)
	defer mem.dynamic_arena_destroy(&arena)
	context.allocator = mem.dynamic_arena_allocator(&arena)
	fmt.println("nocontract-ready")
	for {
		line, failure := bufio.reader_read_bytes(&reader, '\n')
		if len(line) > 0 {
			switch reply in dispatch(line) {
			case Encoded:
				fmt.println(reply.line)
			case Wire_Refused:
				switch refusal in json_reply(struct #all_or_none {refused: Wire_Refused}{refused = reply}) {
				case Encoded:
					fmt.println(refusal.line)
				case Wire_Refused:
					fmt.eprintln(refusal.reason)
					os.exit(1)
				}
			}
		}
		mem.dynamic_arena_reset(&arena)
		switch failure {
		case .None:
		case .EOF:
			return
		case .Unexpected_EOF, .Short_Write, .Invalid_Write, .Short_Buffer, .No_Progress, .Invalid_Whence, .Invalid_Offset, .Invalid_Unread, .Negative_Read, .Negative_Write, .Negative_Count, .Buffer_Full, .Unknown, .No_Size, .Permission_Denied, .Closed, .Unsupported:
			fmt.eprintln("reading native request:", failure)
			os.exit(1)
		}
	}
}
`
