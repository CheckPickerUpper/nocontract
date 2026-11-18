package description

import (
	"math"
	"strconv"
)

// IntegerWidth is the whole numbers an integer holds: signed or unsigned, at 8, 16, 32 or 64 bits.
// Least and Most are exact decimal text, so the most an unsigned 64-bit integer holds is stated
// whole, which no float64 can hold. Every width holds 0.
//
//sumtype:decl
type IntegerWidth interface {
	isIntegerWidth()
	// Least is the least whole number the width holds, in decimal.
	Least() string
	// Most is the most whole number the width holds, in decimal.
	Most() string
	// Wire is how JSON carries a whole number of the width.
	Wire() IntegerWire
}

// IntegerWire is how JSON carries a whole number: as a JSON number, or as a JSON string holding
// its decimal text.
//
//sumtype:decl
type IntegerWire interface{ isIntegerWire() }

// NumberWire is a whole number written as a JSON number. JavaScript reads every JSON number as a
// float64, which holds every whole number of 32 bits or fewer exactly.
//
//ergo:not-an-error
type NumberWire struct{}

// DecimalTextWire is a whole number written as a JSON string holding its canonical decimal text:
// an optional -, then 0 or a digit from 1 to 9 followed by any digits, and never -0. A float64
// holds no whole number past 2^53 exactly, so 64-bit widths travel as text, as protobuf's JSON
// mapping carries int64 and uint64. Pattern is the regular expression taking exactly the canonical
// decimal text of the numbers the width holds, which every client's schema states.
//
//ergo:not-an-error
type DecimalTextWire struct {
	Pattern string
}

func (NumberWire) isIntegerWire()      {}
func (DecimalTextWire) isIntegerWire() {}

// Signed8 holds -128 to 127.
//
//ergo:not-an-error
type Signed8 struct{}

// Signed16 holds -32768 to 32767.
//
//ergo:not-an-error
type Signed16 struct{}

// Signed32 holds -2147483648 to 2147483647.
//
//ergo:not-an-error
type Signed32 struct{}

// Signed64 holds -9223372036854775808 to 9223372036854775807.
//
//ergo:not-an-error
type Signed64 struct{}

// Unsigned8 holds 0 to 255.
//
//ergo:not-an-error
type Unsigned8 struct{}

// Unsigned16 holds 0 to 65535.
//
//ergo:not-an-error
type Unsigned16 struct{}

// Unsigned32 holds 0 to 4294967295.
//
//ergo:not-an-error
type Unsigned32 struct{}

// Unsigned64 holds 0 to 18446744073709551615.
//
//ergo:not-an-error
type Unsigned64 struct{}

func (Signed8) isIntegerWidth()    {}
func (Signed16) isIntegerWidth()   {}
func (Signed32) isIntegerWidth()   {}
func (Signed64) isIntegerWidth()   {}
func (Unsigned8) isIntegerWidth()  {}
func (Unsigned16) isIntegerWidth() {}
func (Unsigned32) isIntegerWidth() {}
func (Unsigned64) isIntegerWidth() {}

// Least is math.MinInt8.
func (Signed8) Least() string { return strconv.FormatInt(math.MinInt8, 10) }

// Most is math.MaxInt8.
func (Signed8) Most() string { return strconv.FormatInt(math.MaxInt8, 10) }

// Least is math.MinInt16.
func (Signed16) Least() string { return strconv.FormatInt(math.MinInt16, 10) }

// Most is math.MaxInt16.
func (Signed16) Most() string { return strconv.FormatInt(math.MaxInt16, 10) }

// Least is math.MinInt32.
func (Signed32) Least() string { return strconv.FormatInt(math.MinInt32, 10) }

// Most is math.MaxInt32.
func (Signed32) Most() string { return strconv.FormatInt(math.MaxInt32, 10) }

// Least is math.MinInt64.
func (Signed64) Least() string { return strconv.FormatInt(math.MinInt64, 10) }

// Most is math.MaxInt64.
func (Signed64) Most() string { return strconv.FormatInt(math.MaxInt64, 10) }

// Least is 0.
func (Unsigned8) Least() string { return strconv.FormatUint(0, 10) }

// Most is math.MaxUint8.
func (Unsigned8) Most() string { return strconv.FormatUint(math.MaxUint8, 10) }

// Least is 0.
func (Unsigned16) Least() string { return strconv.FormatUint(0, 10) }

// Most is math.MaxUint16.
func (Unsigned16) Most() string { return strconv.FormatUint(math.MaxUint16, 10) }

// Least is 0.
func (Unsigned32) Least() string { return strconv.FormatUint(0, 10) }

// Most is math.MaxUint32.
func (Unsigned32) Most() string { return strconv.FormatUint(math.MaxUint32, 10) }

// Least is 0.
func (Unsigned64) Least() string { return strconv.FormatUint(0, 10) }

// Most is math.MaxUint64.
func (Unsigned64) Most() string { return strconv.FormatUint(math.MaxUint64, 10) }

// Wire is a JSON number.
func (Signed8) Wire() IntegerWire { return NumberWire{} }

// Wire is a JSON number.
func (Signed16) Wire() IntegerWire { return NumberWire{} }

// Wire is a JSON number.
func (Signed32) Wire() IntegerWire { return NumberWire{} }

// Wire is decimal text.
func (width Signed64) Wire() IntegerWire {
	return DecimalTextWire{Pattern: decimalTextPattern(width.Least(), width.Most())}
}

// Wire is a JSON number.
func (Unsigned8) Wire() IntegerWire { return NumberWire{} }

// Wire is a JSON number.
func (Unsigned16) Wire() IntegerWire { return NumberWire{} }

// Wire is a JSON number.
func (Unsigned32) Wire() IntegerWire { return NumberWire{} }

// Wire is decimal text.
func (width Unsigned64) Wire() IntegerWire {
	return DecimalTextWire{Pattern: decimalTextPattern(width.Least(), width.Most())}
}
