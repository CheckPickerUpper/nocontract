package nocontract

import "core:encoding/json"
import "core:fmt"
import "core:math"
import "core:strconv"

import "httpbinding"

// Body_Started is the standard JSON parser at the first token of a nonempty body.
Body_Started :: struct #all_or_none {
	parser: json.Parser,
}

// Body_Start is the body parser or the refusal of input with no readable first token.
Body_Start :: union #no_nil {
	Body_Started,
	Request_Refusal,
}

// Integer_Encoding is the wire shape selected by the checked integer width.
Integer_Encoding :: enum {
	JSON_Number,
	Decimal_String,
}

// Body_Finished is a body whose single declared value was completely read.
Body_Finished :: struct #all_or_none {}

// Body_Expectation describes the JSON value a generated reader requires at one path.
Body_Expectation :: distinct string

// start_request_body starts the parser once, after preserving transport failures and partial input.
@(require_results)
start_request_body :: proc(receipt: httpbinding.Body_Receipt) -> Body_Start {
	switch received in receipt.reading {
	case httpbinding.Received_Body:
		return start_body(receipt.text)
	case httpbinding.Body_Not_Received:
		return Request_Refusal(Body_Transport_Refusal{partial = receipt.text, failure = received})
	}
	unreachable()
}

// Canonical_Decimal is decimal text accepted by the standard JSON integer tokenizer.
Canonical_Decimal :: struct #all_or_none {
	value: string,
}

// Decimal_Parse_Refusal preserves the original text, observed token and tokenizer error.
Decimal_Parse_Refusal :: struct #all_or_none {
	parser: json.Parser,
}

// Decimal_Reading distinguishes committed canonical text from the original rejected parser.
Decimal_Reading :: union #no_nil {
	Canonical_Decimal,
	Decimal_Parse_Refusal,
}

// start_body starts the one parser generated body readers share.
@(require_results)
start_body :: proc(text: string) -> Body_Start {
	parser := json.make_parser(text, .JSON, parse_integers = true, allocator = context.temp_allocator)
	switch parser.curr_token.kind {
	case .EOF:
		return Request_Refusal(Body_Problem{path = "body", kind = Body_Empty{}})
	case .Invalid, .Null, .False, .True, .Infinity, .NaN, .Ident, .Integer, .Float, .String,
		.Colon, .Comma, .Open_Brace, .Close_Brace, .Open_Bracket, .Close_Bracket:
	}
	if cause := parser.curr_token_err; cause != .None {
		return malformed_body(&parser, "body", cause)
	}
	return Body_Started{parser = parser}
}

// finish_body refuses a second JSON value after the generated reader consumed the first.
@(require_results)
finish_body :: proc(parser: ^json.Parser) -> Reading(Body_Finished) {
	switch parser.curr_token.kind {
	case .EOF:
		return Read_Value(Body_Finished){value = {}}
	case .Invalid, .Null, .False, .True, .Infinity, .NaN, .Ident, .Integer, .Float, .String,
		.Colon, .Comma, .Open_Brace, .Close_Brace, .Open_Bracket, .Close_Bracket:
		return Request_Refusal(Body_Problem{path = "body", kind = Body_Not_Single{}})
	}
	unreachable()
}

@(private, require_results)
malformed_body :: proc(parser: ^json.Parser, path: string, cause: json.Error) -> Request_Refusal {
	return Body_Problem{path = path, kind = Body_Malformed{parser = parser^, cause = cause}}
}

// consume_body_token advances the standard parser and preserves any error at its new token.
@(require_results)
consume_body_token :: proc(parser: ^json.Parser, path: string) -> Reading(json.Token) {
	previous, failure := json.advance_token(parser)
	if failure != .None && failure != .EOF {
		return malformed_body(parser, path, failure)
	}
	return Read_Value(json.Token){value = previous}
}

// expect_body_token reads one known token kind without reparsing the body.
@(require_results)
expect_body_token :: proc(parser: ^json.Parser, path: string, kind: json.Token_Kind, wanted: Body_Expectation) -> Reading(json.Token) {
	if parser.curr_token.kind != kind {
		return Request_Refusal(Body_Problem{path = path, kind = Body_Value_Mistyped{want = string(wanted), read = Read_Text{value = parser.curr_token.text}}})
	}
	return consume_body_token(parser, path)
}

// read_body_text reads JSON string escaping through the standard library.
@(require_results)
read_body_text :: proc(parser: ^json.Parser, path: string) -> Reading(string) {
	return read_body_text_as(parser, path, "a string")
}

@(private, require_results)
read_body_text_as :: proc(parser: ^json.Parser, path: string, wanted: Body_Expectation) -> Reading(string) {
	switch token in expect_body_token(parser, path, .String, wanted) {
	case Read_Value(json.Token):
		text, failure := json.unquote_string(token.value, .JSON, context.temp_allocator)
		if failure != .None {
			return malformed_body(parser, path, failure)
		}
		return Read_Value(string){value = text}
	case Request_Refusal:
		return token
	}
	unreachable()
}

// read_body_truth reads the two JSON boolean tokens.
@(require_results)
read_body_truth :: proc(parser: ^json.Parser, path: string) -> Reading(bool) {
	switch parser.curr_token.kind {
	case .True:
		switch read in consume_body_token(parser, path) {
		case Read_Value(json.Token): return Read_Value(bool){value = true}
		case Request_Refusal: return read
		}
	case .False:
		switch read in consume_body_token(parser, path) {
		case Read_Value(json.Token): return Read_Value(bool){value = false}
		case Request_Refusal: return read
		}
	case .Invalid, .EOF, .Null, .Infinity, .NaN, .Ident, .Integer, .Float, .String, .Colon, .Comma,
		.Open_Brace, .Close_Brace, .Open_Bracket, .Close_Bracket:
		return Request_Refusal(Body_Problem{path = path, kind = Body_Value_Mistyped{want = "true or false", read = Read_Text{value = parser.curr_token.text}}})
	}
	unreachable()
}

// read_body_number reads a finite JSON number into its native floating-point type.
@(require_results)
read_body_number :: proc(parser: ^json.Parser, path: string) -> Reading(f64) {
	switch parser.curr_token.kind {
	case .Integer, .Float:
		switch token in consume_body_token(parser, path) {
		case Read_Value(json.Token):
			parsed, accepted := strconv.parse_f64(token.value.text)
			if accepted && !math.is_nan(parsed) && !math.is_inf(parsed) {
				return Read_Value(f64){value = parsed}
			}
			return Request_Refusal(Body_Problem{path = path, kind = Body_Value_Mistyped{want = "a finite number", read = Read_Text{value = token.value.text}}})
		case Request_Refusal:
			return token
		}
	case .Invalid, .EOF, .Null, .False, .True, .Infinity, .NaN, .Ident, .String, .Colon, .Comma,
		.Open_Brace, .Close_Brace, .Open_Bracket, .Close_Bracket:
		return Request_Refusal(Body_Problem{path = path, kind = Body_Value_Mistyped{want = "a number", read = Read_Text{value = parser.curr_token.text}}})
	}
	unreachable()
}

// read_body_integer_text reads the wire representation selected by a checked integer width.
@(require_results)
read_body_integer_text :: proc(parser: ^json.Parser, path: string, encoding: Integer_Encoding) -> Reading(string) {
	switch encoding {
	case .Decimal_String:
		switch text in read_body_text_as(parser, path, "a whole number written as a string") {
		case Read_Value(string):
			switch canonical in canonical_decimal(text.value) {
			case Canonical_Decimal:
				return Read_Value(string){value = canonical.value}
			case Decimal_Parse_Refusal:
				return Request_Refusal(Body_Problem{path = path, kind = Body_Decimal_Not_Canonical{decimal = canonical}})
			}
		case Request_Refusal:
			return text
		}
	case .JSON_Number:
		switch parser.curr_token.kind {
		case .Integer:
			switch token in consume_body_token(parser, path) {
			case Read_Value(json.Token): return Read_Value(string){value = token.value.text}
			case Request_Refusal: return token
			}
		case .Float:
			return Request_Refusal(Body_Problem{path = path, kind = Body_Number_Not_Whole{read = Read_Text{value = parser.curr_token.text}}})
		case .Invalid, .EOF, .Null, .False, .True, .Infinity, .NaN, .Ident, .String, .Colon, .Comma,
			.Open_Brace, .Close_Brace, .Open_Bracket, .Close_Bracket:
			return Request_Refusal(Body_Problem{path = path, kind = Body_Value_Mistyped{want = "a whole number", read = Read_Text{value = parser.curr_token.text}}})
		}
	}
	unreachable()
}

// canonical_decimal parses the JSON integer grammar for the decimal-string domain.
// Negative zero is valid JSON but has no canonical NoContract representation.
@(require_results)
canonical_decimal :: proc(text: string) -> Decimal_Reading {
	parser := json.make_parser(text, .JSON, parse_integers = true, allocator = context.temp_allocator)
	if parser.curr_token_err != .None {
		return Decimal_Parse_Refusal{parser = parser}
	}
	switch parser.curr_token.kind {
	case .Integer:
		if parser.curr_token.text == text && text != "-0" {
			return Canonical_Decimal{value = text}
		}
	case .Invalid, .EOF, .Null, .False, .True, .Infinity, .NaN, .Ident, .Float, .String,
		.Colon, .Comma, .Open_Brace, .Close_Brace, .Open_Bracket, .Close_Bracket:
		return Decimal_Parse_Refusal{parser = parser}
	}
	return Decimal_Parse_Refusal{parser = parser}
}

@(private, require_results)
integer_spelling :: proc(text: string, encoding: Integer_Encoding) -> Read_Text {
	switch encoding {
	case .JSON_Number:
		return Read_Text{value = text}
	case .Decimal_String:
		return Read_Text{value = fmt.tprintf("%q", text)}
	}
	unreachable()
}

// read_body_signed reads a declared signed width from its checked wire representation.
@(require_results)
read_body_signed :: proc(parser: ^json.Parser, path: string, encoding: Integer_Encoding, range: Signed_Integer_Range) -> Reading(i64) {
	switch text in read_body_integer_text(parser, path, encoding) {
	case Read_Value(string):
		parsed, accepted := strconv.parse_i64(text.value, 10)
		if accepted && parsed >= range.least && parsed <= range.most {
			return Read_Value(i64){value = parsed}
		}
		return Request_Refusal(Body_Problem{path = path, kind = Body_Number_Outside{read = integer_spelling(text.value, encoding), range = Integer_Range{least = fmt.tprintf("%d", range.least), most = fmt.tprintf("%d", range.most)}}})
	case Request_Refusal:
		return text
	}
	unreachable()
}

// read_body_unsigned reads a declared unsigned width from its checked wire representation.
@(require_results)
read_body_unsigned :: proc(parser: ^json.Parser, path: string, encoding: Integer_Encoding, range: Unsigned_Integer_Range) -> Reading(u64) {
	switch text in read_body_integer_text(parser, path, encoding) {
	case Read_Value(string):
		spelling := text.value
		switch encoding {
		case .JSON_Number:
			if spelling == "-0" {
				spelling = "0"
			}
		case .Decimal_String:
		}
		parsed, accepted := strconv.parse_u64(spelling, 10)
		if accepted && parsed >= range.least && parsed <= range.most {
			return Read_Value(u64){value = parsed}
		}
		return Request_Refusal(Body_Problem{path = path, kind = Body_Number_Outside{read = integer_spelling(text.value, encoding), range = Integer_Range{least = fmt.tprintf("%d", range.least), most = fmt.tprintf("%d", range.most)}}})
	case Request_Refusal:
		return text
	}
	unreachable()
}

// read_body_instant parses a full timestamp and writes its UTC representation through core:time.
@(require_results)
read_body_instant :: proc(parser: ^json.Parser, path: string) -> Reading(string) {
	switch text in read_body_text(parser, path) {
	case Read_Value(string):
		switch instant in read_instant(text.value) {
		case UTC_Instant:
			return Read_Value(string){value = instant.text}
		case Instant_Not_Read:
			return Request_Refusal(Body_Problem{path = path, kind = Body_Instant_Unreadable{instant = instant}})
		}
	case Request_Refusal:
		return text
	}
	unreachable()
}
