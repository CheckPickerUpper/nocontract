package description

import (
	"strconv"
	"strings"
)

// decimalTextPattern is the pattern of canonical decimal text holding exactly the whole numbers from
// least to most, decimal text holding 0: 0, each positive number up to most, and - before each
// positive number up to least's magnitude.
func decimalTextPattern(least, most string) string {
	alternatives := []string{"0"}
	if most != "0" {
		alternatives = append(alternatives, positiveUpTo(most))
	}
	if magnitude, isNegative := strings.CutPrefix(least, "-"); isNegative {
		alternatives = append(alternatives, "-(?:"+positiveUpTo(magnitude)+")")
	}
	return "^(?:" + strings.Join(alternatives, "|") + ")$"
}

// positiveUpTo is the alternatives matching every whole number from 1 to most, decimal text of a
// positive number, each written without a leading zero: every shorter number, then each number of
// most's length by the first place it is below most at, and most itself through its last place.
func positiveUpTo(most string) string {
	alternatives := []string{}
	if len(most) > 1 {
		alternatives = append(alternatives, "[1-9]"+digitsUpTo(len(most)-2))
	}
	for place := range len(most) {
		low, high := byte('0'), most[place]
		if place == 0 {
			low = '1'
		}
		if place < len(most)-1 {
			high--
		}
		if high < low {
			continue
		}
		alternatives = append(alternatives, most[:place]+digitBetween(low, high)+anyDigits(len(most)-place-1))
	}
	return strings.Join(alternatives, "|")
}

// digitBetween matches one digit from low to high.
func digitBetween(low, high byte) string {
	if low == high {
		return string(low)
	}
	return "[" + string(low) + "-" + string(high) + "]"
}

// anyDigits matches exactly count digits.
func anyDigits(count int) string {
	switch count {
	case 0:
		return ""
	case 1:
		return "[0-9]"
	}
	return "[0-9]{" + strconv.Itoa(count) + "}"
}

// digitsUpTo matches from no digit to most digits.
func digitsUpTo(most int) string {
	switch most {
	case 0:
		return ""
	case 1:
		return "[0-9]?"
	}
	return "[0-9]{0," + strconv.Itoa(most) + "}"
}
