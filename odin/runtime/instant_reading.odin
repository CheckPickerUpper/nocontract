package nocontract

import "core:fmt"
import "core:strings"
import "core:time"
import dt "core:time/datetime"

// UTC_Instant is a checked RFC 3339 instant written with UTC and nanosecond precision.
UTC_Instant :: struct #all_or_none {
	text: string,
}

// Instant_Wire_Not_Declared is a timestamp outside the shared RFC 3339 wire syntax.
Instant_Wire_Not_Declared :: struct #all_or_none {}

// Instant_Not_Parsed keeps how much input the standard ISO parser consumed.
Instant_Not_Parsed :: struct #all_or_none {
	consumed: int,
}

// Instant_Calendar_Refused keeps the standard calendar's original validation failure.
Instant_Calendar_Refused :: struct #all_or_none {
	cause: dt.Error,
}

// Instant_UTC_Outside_Wire keeps a UTC year RFC 3339 cannot represent.
Instant_UTC_Outside_Wire :: struct #all_or_none {
	calendar: dt.DateTime,
}

// Instant_Cause distinguishes syntax, parsing, calendar and UTC representation failures.
Instant_Cause :: union #no_nil {
	Instant_Wire_Not_Declared,
	Instant_Not_Parsed,
	Instant_Calendar_Refused,
	Instant_UTC_Outside_Wire,
}

// Instant_Not_Read preserves the original timestamp and its exact failure.
Instant_Not_Read :: struct #all_or_none {
	input: string,
	cause: Instant_Cause,
}

// Instant_Reading is a canonical UTC timestamp or the evidence that refused it.
Instant_Reading :: union #no_nil {
	UTC_Instant,
	Instant_Not_Read,
}

// read_instant uses the standard ISO parser and calendar at the RFC 3339 wire boundary.
// Calendar values preserve years outside time.Time's Unix-nanosecond range.
@(require_results)
read_instant :: proc(input: string) -> Instant_Reading {
	switch wire in read_instant_wire(input) {
	case RFC3339_Text:
		return read_instant_calendar(wire)
	case Instant_Wire_Not_Declared:
		return Instant_Not_Read{input = input, cause = wire}
	}
	unreachable()
}

@(private)
RFC3339_Text :: struct #all_or_none {
	value: string,
}

@(private)
Instant_Wire :: union #no_nil {
	RFC3339_Text,
	Instant_Wire_Not_Declared,
}

@(private, require_results)
read_instant_calendar :: proc(wire: RFC3339_Text) -> Instant_Reading {
	input := wire.value
	calendar, offset, _, consumed := time.iso8601_to_components(input)
	if consumed != len(input) || consumed == 0 {
		return Instant_Not_Read{input = input, cause = Instant_Not_Parsed{consumed = consumed}}
	}
	switch cause := dt.validate_datetime(calendar); cause {
	case .None:
	case .Invalid_Year, .Invalid_Month, .Invalid_Day, .Invalid_Hour, .Invalid_Minute,
		.Invalid_Second, .Invalid_Nano, .Invalid_Ordinal, .Invalid_Delta:
		return Instant_Not_Read{input = input, cause = Instant_Calendar_Refused{cause = cause}}
	}
	utc, cause := dt.add_delta_to_datetime(calendar, dt.Delta{days = 0, seconds = -i64(offset) * 60, nanos = 0})
	switch cause {
	case .None:
	case .Invalid_Year, .Invalid_Month, .Invalid_Day, .Invalid_Hour, .Invalid_Minute,
		.Invalid_Second, .Invalid_Nano, .Invalid_Ordinal, .Invalid_Delta:
		return Instant_Not_Read{input = input, cause = Instant_Calendar_Refused{cause = cause}}
	}
	if utc.year < 0 || utc.year > 9999 {
		return Instant_Not_Read{input = input, cause = Instant_UTC_Outside_Wire{calendar = utc}}
	}
	fraction := ""
	if utc.nano != 0 {
		fraction = fmt.tprintf(".%s", strings.trim_right(fmt.tprintf("%09d", utc.nano), "0"))
	}
	return UTC_Instant{text = fmt.tprintf("%04d-%02d-%02dT%02d:%02d:%02d%sZ", utc.year, utc.month, utc.day, utc.hour, utc.minute, utc.second, fraction)}
}

@(private, require_results)
read_instant_wire :: proc(input: string) -> Instant_Wire {
	if len(input) < 20 || input[10] != 'T' || input[17:19] == "60" {
		return Instant_Wire_Not_Declared{}
	}
	if input[4] != '-' || input[7] != '-' || input[13] != ':' || input[16] != ':' {
		return Instant_Wire_Not_Declared{}
	}
	digit_positions := []int{0, 1, 2, 3, 5, 6, 8, 9, 11, 12, 14, 15, 17, 18}
	for index in digit_positions {
		if input[index] < '0' || input[index] > '9' {
			return Instant_Wire_Not_Declared{}
		}
	}
	zone := 19
	if input[zone] == '.' {
		zone += 1
		first := zone
		for zone < len(input) && input[zone] >= '0' && input[zone] <= '9' {
			zone += 1
		}
		if zone == first || zone - first > 9 || zone == len(input) {
			return Instant_Wire_Not_Declared{}
		}
	}
	switch input[zone] {
	case 'Z':
		if zone + 1 == len(input) {
			return RFC3339_Text{value = input}
		}
	case '+', '-':
		if len(input) - zone != 6 || input[zone+3] != ':' {
			return Instant_Wire_Not_Declared{}
		}
		hour_tens, hour_units := input[zone+1], input[zone+2]
		minute_tens, minute_units := input[zone+4], input[zone+5]
		if hour_tens >= '0' && hour_tens <= '2' && hour_units >= '0' && hour_units <= '9' &&
			(hour_tens != '2' || hour_units <= '3') && minute_tens >= '0' && minute_tens <= '5' &&
			minute_units >= '0' && minute_units <= '9' {
			return RFC3339_Text{value = input}
		}
	}
	return Instant_Wire_Not_Declared{}
}
