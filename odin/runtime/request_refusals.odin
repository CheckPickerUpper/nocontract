package nocontract

import "core:encoding/json"
import "core:fmt"
import "core:strings"

import "httpbinding"

// Read_Value is a request value in the exact type its native handler takes.
Read_Value :: struct($T: typeid) #all_or_none {
	value: T,
}

// Reading is a parsed request value or the evidence that prevented it being read.
Reading :: union($T: typeid) #no_nil {
	Read_Value(T),
	Request_Refusal,
}

// Parameter_Part names the request parts that contain text parameters.
Parameter_Part :: enum {
	Path,
	Query,
	Header,
}

// Parameter_Text is a named text parameter and the exact text it held.
Parameter_Text :: struct #all_or_none {
	part: Parameter_Part,
	name: string,
	text: string,
}

// Missing_Query is a query parameter absent from the request.
Missing_Query :: struct #all_or_none {
	name: string,
	query: Query_Parameters,
}

// Missing_Header is a declared header absent from the request's parsed headers.
Missing_Header :: struct #all_or_none {
	name:    string,
	headers: map[string]string,
}

// Decimal_Not_Canonical is text outside NoContract's canonical whole-number encoding.
Decimal_Not_Canonical :: struct #all_or_none {
	decimal: Decimal_Parse_Refusal,
}

// Integer_Range is the least and most decimal values a declared field accepts.
Integer_Range :: struct #all_or_none {
	least: string,
	most:  string,
}

// Signed_Integer_Range is emitted as numeric literals from the checked signed width.
Signed_Integer_Range :: struct #all_or_none {
	least: i64,
	most:  i64,
}

// Unsigned_Integer_Range is emitted as numeric literals from the checked unsigned width.
Unsigned_Integer_Range :: struct #all_or_none {
	least: u64,
	most:  u64,
}

// Integer_Parser identifies the standard decimal parser whose range was exceeded.
@(not_an_error)
Integer_Parser :: enum {
	Signed,
	Unsigned,
}

// Parameter_Outside_Range keeps the range and parser of an unreadable whole number.
Parameter_Outside_Range :: struct #all_or_none {
	range:  Integer_Range,
	parser: Integer_Parser,
}

// Parameter_Literal_Outside keeps the declared values an unreadable literal could have held.
Parameter_Literal_Outside :: struct #all_or_none {
	values: []string,
}

// Parameter_Truth_Unreadable is text the boolean parser does not accept.
Parameter_Truth_Unreadable :: struct #all_or_none {}

// Parameter_Problem is the rule that a present parameter's text did not satisfy.
Parameter_Problem :: union #no_nil {
	Decimal_Not_Canonical,
	Parameter_Outside_Range,
	Parameter_Literal_Outside,
	Parameter_Truth_Unreadable,
}

// Parameter_Refusal keeps the present parameter and the exact rule that refused it.
Parameter_Refusal :: struct #all_or_none {
	source:  Parameter_Text,
	problem: Parameter_Problem,
}

// Path_Not_Decoded is a URL segment the standard percent decoder could not read.
Path_Not_Decoded :: struct #all_or_none {
	segment: string,
	failure: Percent_Not_Decoded,
}

// Read_Text keeps the original spelling of a value refused by a body reader.
Read_Text :: struct #all_or_none {
	value: string,
}

// Body_Member_Missing is a declared member absent from its object.
Body_Member_Missing :: struct #all_or_none {}

// Body_Member_Repeated is a member written twice in one object.
Body_Member_Repeated :: struct #all_or_none {}

// Body_Member_Unknown is a member outside the object's declared fields.
Body_Member_Unknown :: struct #all_or_none {}

// Body_Value_Mistyped keeps the expected JSON shape and the value read instead.
Body_Value_Mistyped :: struct #all_or_none {
	want: string,
	read: Read_Text,
}

// Body_Literal_Outside keeps a text value and the literals its field accepts.
Body_Literal_Outside :: struct #all_or_none {
	read:   Read_Text,
	values: []string,
}

// Body_Key_Outside keeps an object key and the declared keys its dictionary accepts.
Body_Key_Outside :: struct #all_or_none {
	read: Read_Text,
	keys: []string,
}

// Body_Number_Not_Whole is a numeric value written with a fraction or an exponent.
Body_Number_Not_Whole :: struct #all_or_none {
	read: Read_Text,
}

// Body_Number_Outside keeps a numeric value and the bounds its field accepts.
Body_Number_Outside :: struct #all_or_none {
	read:  Read_Text,
	range: Integer_Range,
}

// Body_Decimal_Not_Canonical is a 64-bit field's string outside canonical decimal text.
Body_Decimal_Not_Canonical :: struct #all_or_none {
	decimal: Decimal_Parse_Refusal,
}

// Body_Instant_Unreadable keeps the original timestamp and its distinct parser failure.
Body_Instant_Unreadable :: struct #all_or_none {
	instant: Instant_Not_Read,
}

// Body_List_Length_Wrong is a list holding a different number of values from its fixed length.
Body_List_Length_Wrong :: struct #all_or_none {
	wanted: int,
	read:   int,
}

// Body_Malformed keeps the original JSON parser, including its input, position and token.
Body_Malformed :: struct #all_or_none {
	parser: json.Parser,
	cause:  json.Error,
}

// Body_Empty is a request containing no JSON value.
Body_Empty :: struct #all_or_none {}

// Body_Not_Single is input containing another value after its declared body.
Body_Not_Single :: struct #all_or_none {}

// Body_Problem_Kind distinguishes every semantic and syntax refusal of a JSON body.
Body_Problem_Kind :: union #no_nil {
	Body_Member_Missing,
	Body_Member_Repeated,
	Body_Member_Unknown,
	Body_Value_Mistyped,
	Body_Literal_Outside,
	Body_Key_Outside,
	Body_Number_Not_Whole,
	Body_Number_Outside,
	Body_Decimal_Not_Canonical,
	Body_Instant_Unreadable,
	Body_List_Length_Wrong,
	Body_Malformed,
	Body_Empty,
	Body_Not_Single,
}

// Body_Problem holds the one JSON path shared by its distinct refusal kinds.
Body_Problem :: struct #all_or_none {
	path: string,
	kind: Body_Problem_Kind,
}

// Body_Transport_Refusal preserves the partial body and the original transport failure.
Body_Transport_Refusal :: struct #all_or_none {
	partial: string,
	failure: httpbinding.Body_Not_Received,
}

// Body_Size_Refusal retains the native size receipt and the payload received before refusal.
Body_Size_Refusal :: struct #all_or_none {
	partial: string,
	failure: httpbinding.Body_Size_Exceeded,
}

// Request_Refusal preserves each cause a generated request reader can return.
Request_Refusal :: union #no_nil {
	Missing_Query,
	Missing_Header,
	Parameter_Refusal,
	Path_Not_Decoded,
	Body_Problem,
	Body_Transport_Refusal,
	Body_Size_Refusal,
}

// Request_Refused_Fields is the projection the shared RequestRefused wire type accepts.
Request_Refused_Fields :: struct #all_or_none {
	part:   string,
	name:   string,
	reason: string,
}

// Body_Size_Source identifies the source of the size published in the refusal.
Body_Size_Source :: enum {
	Content_Length,
	Stream,
}

// Body_Too_Large_Fields is the common size refusal's projection from the native receipt.
Body_Too_Large_Fields :: struct #all_or_none {
	limit_bytes: u64,
	size_bytes: u64,
	size_source: Body_Size_Source,
}

// Refusal_Fields keeps size refusal fields separate from unreadable request fields.
Refusal_Fields :: union #no_nil {
	Request_Refused_Fields,
	Body_Too_Large_Fields,
}

// refusal_fields describes a refusal while its original typed evidence remains on the refusal.
@(require_results)
refusal_fields :: proc(refusal: Request_Refusal) -> Refusal_Fields {
	switch refused in refusal {
	case Missing_Query:
		return Request_Refused_Fields{part = "query", name = refused.name, reason = "the query parameter is missing"}
	case Missing_Header:
		return Request_Refused_Fields{part = "header", name = refused.name, reason = "the header is missing"}
	case Parameter_Refusal:
		return Request_Refused_Fields{part = strings.to_lower(fmt.tprintf("%v", refused.source.part)), name = refused.source.name, reason = parameter_reason(refused)}
	case Path_Not_Decoded:
		switch cause in refused.failure.cause {
		case Percent_Triplet_Invalid:
			return Request_Refused_Fields{part = "path", name = refused.segment, reason = fmt.tprintf("invalid URL escape %q", cause.escape)}
		case Percent_Decoder_Refused:
			return Request_Refused_Fields{part = "path", name = refused.segment, reason = fmt.tprintf("the URL decoder refused %q", refused.failure.input)}
		}
	case Body_Problem:
		return Request_Refused_Fields{part = "body", name = refused.path, reason = body_reason(refused)}
	case Body_Transport_Refusal:
		return Request_Refused_Fields{part = "body", name = "body", reason = fmt.tprintf("The body could not be read: %v.", refused.failure.cause)}
	case Body_Size_Refusal:
		cause := refused.failure.cause
		switch cause.framing {
		case .Content_Length:
			return Body_Too_Large_Fields{limit_bytes = u64(cause.max_length), size_bytes = u64(cause.next_length), size_source = .Content_Length}
		case .Chunked:
			return Body_Too_Large_Fields{limit_bytes = u64(cause.max_length), size_bytes = u64(cause.max_length) + 1, size_source = .Stream}
		}
	}
	unreachable()
}

@(private, require_results)
parameter_reason :: proc(refusal: Parameter_Refusal) -> string {
	switch problem in refusal.problem {
	case Decimal_Not_Canonical:
		return fmt.tprintf("%q is not a whole number written as canonical decimal text: an optional -, then 0 or a digit from 1 to 9 followed by any digits, and never -0.", refusal.source.text)
	case Parameter_Outside_Range:
		parser := "ParseInt"
		switch problem.parser {
		case .Signed:
		case .Unsigned:
			parser = "ParseUint"
		}
		return fmt.tprintf("strconv.%s: parsing %q: value out of range", parser, refusal.source.text)
	case Parameter_Literal_Outside:
		return fmt.tprintf("%q is not one of the values this field accepts.", refusal.source.text)
	case Parameter_Truth_Unreadable:
		return fmt.tprintf("strconv.ParseBool: parsing %q: invalid syntax", refusal.source.text)
	}
	unreachable()
}

@(private, require_results)
body_reason :: proc(problem: Body_Problem) -> string {
	switch cause in problem.kind {
	case Body_Member_Missing:
		return fmt.tprintf("%s is missing from the body.", problem.path)
	case Body_Member_Repeated:
		return fmt.tprintf("%s is written more than once.", problem.path)
	case Body_Member_Unknown:
		return fmt.tprintf("%s is not a member the body accepts.", problem.path)
	case Body_Value_Mistyped:
		return fmt.tprintf("%s must hold %s, and it holds %s.", problem.path, cause.want, cause.read.value)
	case Body_Literal_Outside:
		return fmt.tprintf("%s holds %q, which is not one of the values it accepts.", problem.path, cause.read.value)
	case Body_Key_Outside:
		return fmt.tprintf("%s is under the key %q, which is not one of the keys it accepts.", problem.path, cause.read.value)
	case Body_Number_Not_Whole:
		return fmt.tprintf("%s holds %s, which is not written as a whole number.", problem.path, cause.read.value)
	case Body_Number_Outside:
		return fmt.tprintf("%s holds %s, which is outside %s to %s.", problem.path, cause.read.value, cause.range.least, cause.range.most)
	case Body_Decimal_Not_Canonical:
		return fmt.tprintf("%s holds %q, which is not a whole number written as canonical decimal text: an optional -, then 0 or a digit from 1 to 9 followed by any digits, and never -0.", problem.path, cause.decimal.parser.tok.data)
	case Body_Instant_Unreadable:
		return fmt.tprintf("%s holds %q, which is not an RFC 3339 time.", problem.path, cause.instant.input)
	case Body_List_Length_Wrong:
		return fmt.tprintf("%s must hold exactly %d items, and it holds %d.", problem.path, cause.wanted, cause.read)
	case Body_Malformed:
		return fmt.tprintf("%s is not JSON: %v.", problem.path, cause.cause)
	case Body_Empty:
		return "The body holds no JSON value."
	case Body_Not_Single:
		return "The body holds more than one JSON value."
	}
	unreachable()
}
