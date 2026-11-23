package nocontract

import "core:encoding/json"

// Field_Not_Read distinguishes an absent body member from any decoded value, including null.
@(not_an_error)
Field_Not_Read :: struct #all_or_none {}

// Body_Field is one required field before or after its single successful read.
Body_Field :: union($T: typeid) #no_nil {
	Field_Not_Read,
	Read_Value(T),
}

// Body_Container distinguishes JSON objects from lists at their punctuation boundary.
Body_Container :: enum {
	Object,
	List,
}

// Container_Progress is whether a container has another member or has consumed its close token.
@(not_an_error)
Container_Progress :: enum {
	More,
	Done,
}

@(private)
Body_Opening :: struct #all_or_none {
	token: json.Token_Kind,
	expectation: Body_Expectation,
}

@(private, require_results)
body_opening :: proc(kind: Body_Container) -> Body_Opening {
	switch kind {
	case .Object: return Body_Opening{token = .Open_Brace, expectation = "an object"}
	case .List: return Body_Opening{token = .Open_Bracket, expectation = "a list"}
	}
	unreachable()
}

// begin_body_container consumes an opening token and a possible empty-container closing token.
@(require_results)
begin_body_container :: proc(parser: ^json.Parser, path: string, kind: Body_Container) -> Reading(Container_Progress) {
	opening := body_opening(kind)
	switch opened in expect_body_token(parser, path, opening.token, opening.expectation) {
	case Read_Value(json.Token):
	case Request_Refusal:
		return opened
	}
	switch parser.curr_token.kind {
	case .Close_Brace:
		switch kind {
		case .Object: return close_body_container(parser, path)
		case .List: return malformed_body(parser, path, .Unexpected_Token)
		}
	case .Close_Bracket:
		switch kind {
		case .List: return close_body_container(parser, path)
		case .Object: return malformed_body(parser, path, .Unexpected_Token)
		}
	case .Invalid, .EOF:
		return malformed_body(parser, path, .Unexpected_Token)
	case .Null, .False, .True, .Infinity, .NaN, .Ident, .Integer, .Float, .String, .Colon, .Comma,
		.Open_Brace, .Open_Bracket:
		return Read_Value(Container_Progress){value = .More}
	}
	unreachable()
}

// continue_body_container consumes the comma or closing token following one complete value.
@(require_results)
continue_body_container :: proc(parser: ^json.Parser, path: string, kind: Body_Container) -> Reading(Container_Progress) {
	switch parser.curr_token.kind {
	case .Close_Brace:
		switch kind {
		case .Object: return close_body_container(parser, path)
		case .List: return malformed_body(parser, path, .Unexpected_Token)
		}
	case .Close_Bracket:
		switch kind {
		case .List: return close_body_container(parser, path)
		case .Object: return malformed_body(parser, path, .Unexpected_Token)
		}
	case .Comma:
		switch comma in consume_body_token(parser, path) {
		case Read_Value(json.Token): return Read_Value(Container_Progress){value = .More}
		case Request_Refusal: return comma
		}
	case .Invalid, .EOF, .Null, .False, .True, .Infinity, .NaN, .Ident, .Integer, .Float, .String,
		.Colon, .Open_Brace, .Open_Bracket:
		return malformed_body(parser, path, .Unexpected_Token)
	}
	unreachable()
}

@(private, require_results)
close_body_container :: proc(parser: ^json.Parser, path: string) -> Reading(Container_Progress) {
	switch closed in consume_body_token(parser, path) {
	case Read_Value(json.Token): return Read_Value(Container_Progress){value = .Done}
	case Request_Refusal: return closed
	}
	unreachable()
}

// read_body_member reads a JSON object key and its required colon.
@(require_results)
read_body_member :: proc(parser: ^json.Parser, path: string) -> Reading(string) {
	switch key in read_body_text(parser, path) {
	case Read_Value(string):
		switch colon in expect_body_token(parser, path, .Colon, "a colon after an object key") {
		case Read_Value(json.Token): return key
		case Request_Refusal: return colon
		}
	case Request_Refusal:
		return key
	}
	unreachable()
}
