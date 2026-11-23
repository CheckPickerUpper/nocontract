package nocontract

import "core:encoding/hex"
import "core:fmt"
import "core:net"
import "core:strconv"
import "core:strings"

import http "../dependencies/odin-http"

// Percent_Triplet_Invalid is an incomplete or nonhexadecimal percent escape.
Percent_Triplet_Invalid :: struct #all_or_none {
	escape: string,
}

// Percent_Decoder_Refused is a syntactically valid component the library could not decode.
Percent_Decoder_Refused :: struct #all_or_none {}

// Percent_Cause preserves syntax refusal separately from the library's decoder result.
Percent_Cause :: union #no_nil {
	Percent_Triplet_Invalid,
	Percent_Decoder_Refused,
}

// Percent_Not_Decoded keeps the complete encoded component and the cause of its refusal.
Percent_Not_Decoded :: struct #all_or_none {
	input: string,
	cause: Percent_Cause,
}

// Decoded_Component is the library-decoded text of one URL component.
Decoded_Component :: struct #all_or_none {
	text: string,
}

// Percent_Decoding is a decoded component or its original encoded input and failure.
Percent_Decoding :: union #no_nil {
	Decoded_Component,
	Percent_Not_Decoded,
}

@(private, require_results)
decode_component :: proc(input: string) -> Percent_Decoding {
	for character, index in input {
		if character != '%' {
			continue
		}
		end := min(index + 3, len(input))
		if end - index != 3 {
			return Percent_Not_Decoded{input = input, cause = Percent_Triplet_Invalid{escape = input[index:end]}}
		}
		_, accepted := hex.decode_sequence(input[index+1:end])
		if !accepted {
			return Percent_Not_Decoded{input = input, cause = Percent_Triplet_Invalid{escape = input[index:end]}}
		}
	}
	decoded, accepted := net.percent_decode(input, context.temp_allocator)
	if !accepted {
		return Percent_Not_Decoded{input = input, cause = Percent_Decoder_Refused{}}
	}
	return Decoded_Component{text = decoded}
}

// Request_Path is a request's decoded segments, preserving empty trailing segments.
Request_Path :: struct #all_or_none {
	segments: []string,
}

// read_request_path decodes each segment independently, so an escaped slash stays in its segment.
@(require_results)
read_request_path :: proc(path: string) -> Reading(Request_Path) {
	if path == "" || path == "/" {
		return Read_Value(Request_Path){value = Request_Path{segments = {}}}
	}
	raw := path
	if strings.has_prefix(raw, "/") {
		raw = raw[1:]
	}
	segments := strings.split(raw, "/", context.temp_allocator)
	for &segment in segments {
		switch decoded in decode_component(segment) {
		case Decoded_Component:
			segment = decoded.text
		case Percent_Not_Decoded:
			return Request_Refusal(Path_Not_Decoded{segment = segment, failure = decoded})
		}
	}
	return Read_Value(Request_Path){value = Request_Path{segments = segments}}
}

// Query_Semicolon_Refused is an entry Go's query contract does not accept as a parameter.
Query_Semicolon_Refused :: struct #all_or_none {}

// Query_Key_Not_Decoded is a query key whose percent escapes could not be decoded.
Query_Key_Not_Decoded :: struct #all_or_none {
	failure: Percent_Not_Decoded,
}

// Query_Value_Not_Decoded is a query value whose percent escapes could not be decoded.
Query_Value_Not_Decoded :: struct #all_or_none {
	failure: Percent_Not_Decoded,
}

// Query_Entry_Cause distinguishes each cause of an ignored query entry.
Query_Entry_Cause :: union #no_nil {
	Query_Semicolon_Refused,
	Query_Key_Not_Decoded,
	Query_Value_Not_Decoded,
}

// Query_Entry_Not_Read keeps an ignored entry and its original decoding failure.
Query_Entry_Not_Read :: struct #all_or_none {
	entry: http.Query_Entry,
	cause: Query_Entry_Cause,
}

// Query_Parameters keeps the original query, its first value for each key and every ignored entry.
Query_Parameters :: struct #all_or_none {
	input:   string,
	values:  map[string]string,
	ignored: [dynamic]Query_Entry_Not_Read,
}

// read_query_parameters follows Go's query contract while retaining ignored-entry evidence.
@(require_results)
read_query_parameters :: proc(input: string) -> Query_Parameters {
	result := Query_Parameters{input = input, values = make(map[string]string, context.temp_allocator), ignored = make([dynamic]Query_Entry_Not_Read, context.temp_allocator)}
	remaining := input
	for entry in http.query_iter(&remaining) {
		if strings.contains(entry.key, ";") || strings.contains(entry.value, ";") {
			append(&result.ignored, Query_Entry_Not_Read{entry = entry, cause = Query_Semicolon_Refused{}})
			continue
		}
		key_text, _ := strings.replace_all(entry.key, "+", " ", context.temp_allocator)
		value_text, _ := strings.replace_all(entry.value, "+", " ", context.temp_allocator)
		switch key in decode_component(key_text) {
		case Decoded_Component:
			switch value in decode_component(value_text) {
			case Decoded_Component:
				if key.text not_in result.values {
					result.values[key.text] = value.text
				}
			case Percent_Not_Decoded:
				append(&result.ignored, Query_Entry_Not_Read{entry = entry, cause = Query_Value_Not_Decoded{failure = value}})
			}
		case Percent_Not_Decoded:
			append(&result.ignored, Query_Entry_Not_Read{entry = entry, cause = Query_Key_Not_Decoded{failure = key}})
		}
	}
	return result
}

// query_parameter finds one declared query parameter and keeps the whole query on refusal.
@(require_results)
query_parameter :: proc(query: Query_Parameters, name: string) -> Reading(Parameter_Text) {
	if text, present := query.values[name]; present {
		return Read_Value(Parameter_Text){value = Parameter_Text{part = .Query, name = name, text = text}}
	}
	return Request_Refusal(Missing_Query{name = name, query = query})
}

// header_parameter reads through odin-http's case-insensitive header lookup.
@(require_results)
header_parameter :: proc(headers: map[string]string, name: string) -> Reading(Parameter_Text) {
	if text, present := http.headers_get(http.Headers{_kv = headers, readonly = true}, name); present {
		return Read_Value(Parameter_Text){value = Parameter_Text{part = .Header, name = name, text = text}}
	}
	return Request_Refusal(Missing_Header{name = name, headers = headers})
}

// read_parameter_signed reads canonical decimal text within the declared signed bounds.
@(require_results)
read_parameter_signed :: proc(source: Parameter_Text, range: Signed_Integer_Range) -> Reading(i64) {
	switch canonical in canonical_decimal(source.text) {
	case Canonical_Decimal:
		parsed, accepted := strconv.parse_i64(canonical.value, 10)
		if accepted && parsed >= range.least && parsed <= range.most {
			return Read_Value(i64){value = parsed}
		}
		return Request_Refusal(Parameter_Refusal{source = source, problem = Parameter_Outside_Range{range = Integer_Range{least = fmt.tprintf("%d", range.least), most = fmt.tprintf("%d", range.most)}, parser = .Signed}})
	case Decimal_Parse_Refusal:
		return Request_Refusal(Parameter_Refusal{source = source, problem = Decimal_Not_Canonical{decimal = canonical}})
	}
	unreachable()
}

// read_parameter_unsigned reads canonical decimal text within the declared unsigned bounds.
@(require_results)
read_parameter_unsigned :: proc(source: Parameter_Text, range: Unsigned_Integer_Range) -> Reading(u64) {
	switch canonical in canonical_decimal(source.text) {
	case Canonical_Decimal:
		parsed, accepted := strconv.parse_u64(canonical.value, 10)
		if accepted && parsed >= range.least && parsed <= range.most {
			return Read_Value(u64){value = parsed}
		}
		return Request_Refusal(Parameter_Refusal{source = source, problem = Parameter_Outside_Range{range = Integer_Range{least = fmt.tprintf("%d", range.least), most = fmt.tprintf("%d", range.most)}, parser = .Unsigned}})
	case Decimal_Parse_Refusal:
		return Request_Refusal(Parameter_Refusal{source = source, problem = Decimal_Not_Canonical{decimal = canonical}})
	}
	unreachable()
}

// read_parameter_truth uses the standard boolean spellings shared with Go.
@(require_results)
read_parameter_truth :: proc(source: Parameter_Text) -> Reading(bool) {
	parsed, accepted := strconv.parse_bool(source.text)
	if accepted {
		return Read_Value(bool){value = parsed}
	}
	return Request_Refusal(Parameter_Refusal{source = source, problem = Parameter_Truth_Unreadable{}})
}
