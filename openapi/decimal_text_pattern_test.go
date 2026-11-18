package openapi_test

import (
	"encoding/json"
	"math/big"
	"regexp"
	"testing"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/openapi"
)

// patternedDocument is the pattern of each property of each component schema of a rendered
// document.
type patternedDocument struct {
	Components patternedComponents `json:"components"`
}

// patternedComponents is a rendered document's component schemas, by name.
type patternedComponents struct {
	Schemas map[string]patternedSchema `json:"schemas"`
}

// patternedSchema is the pattern of each property of one component schema, by property name.
type patternedSchema struct {
	Properties map[string]struct {
		Pattern string `json:"pattern"`
	} `json:"properties"`
}

// whole is decimal text as a big.Int, stopping the test when it is not one.
func whole(t *testing.T, decimal string) *big.Int {
	t.Helper()
	read, isWhole := big.NewInt(0).SetString(decimal, 10)
	if !isWhole {
		t.Fatalf("%q is not decimal text", decimal)
	}
	return read
}

// decimalCandidates is text around every edge of the whole numbers from least to most: each bound
// and one either side of it, every digit of each bound changed, each power of ten up to a digit
// past the bound's length and one below it, each of them negated, and text that is not canonical
// decimal.
func decimalCandidates(least, most *big.Int) []string {
	candidates := []string{"", "-", "+7", "007", "-0", "-007", "00", "1.0", "1e3", " 1", "1 ", "0x1", "١"}
	one := big.NewInt(1)
	for _, bound := range []*big.Int{least, most, big.NewInt(0)} {
		for _, near := range []*big.Int{big.NewInt(0).Sub(bound, one), bound, big.NewInt(0).Add(bound, one)} {
			candidates = append(candidates, near.String())
		}
		magnitude := big.NewInt(0).Abs(bound).String()
		for place := range len(magnitude) {
			for digit := '0'; digit <= '9'; digit++ {
				changed := magnitude[:place] + string(digit) + magnitude[place+1:]
				candidates = append(candidates, changed, "-"+changed)
			}
		}
		power := big.NewInt(1)
		for range len(magnitude) + 1 {
			power.Mul(power, big.NewInt(10))
			below := big.NewInt(0).Sub(power, one)
			candidates = append(candidates, power.String(), below.String(), "-"+power.String(), "-"+below.String())
		}
	}
	return candidates
}

// inRange is whether text is the canonical decimal text of a whole number from least to most, as
// math/big reads and writes it.
func inRange(text string, least, most *big.Int) bool {
	read, isNumber := big.NewInt(0).SetString(text, 10)
	return isNumber && read.String() == text && read.Cmp(least) >= 0 && read.Cmp(most) <= 0
}

// Each 64-bit width's published pattern takes exactly the canonical decimal text of the whole
// numbers its width holds: no leading zero, no plus sign, no -0, and nothing one past either bound
// at any digit.
func TestADecimalTextPatternTakesExactlyTheNumbersItsWidthHolds(t *testing.T) {
	t.Parallel()
	written := outcome.Fold(openapi.Render(itemAPI(t), "2.1.0"), func(written []byte) []byte { return written }, func(cause error) []byte {
		t.Fatalf("Render refused: %v", cause)
		return nil
	})
	document := patternedDocument{Components: patternedComponents{Schemas: map[string]patternedSchema{}}}
	outcome.ForEachOption(outcome.EmptyableToOption(json.Unmarshal(written, &document)), func(cause error) { t.Fatalf("reading the document: %v", cause) })
	for _, each := range []struct {
		schema   string
		property string
		width    description.IntegerWidth
	}{
		{schema: "ItemAdded", property: "id", width: description.Signed64{}},
		{schema: "ItemMissing", property: "item_id", width: description.Unsigned64{}},
	} {
		pattern := regexp.MustCompile(document.Components.Schemas[each.schema].Properties[each.property].Pattern)
		least, most := whole(t, each.width.Least()), whole(t, each.width.Most())
		for _, text := range decimalCandidates(least, most) {
			taken, held := pattern.MatchString(text), inRange(text, least, most)
			if taken && !held {
				t.Errorf("%s.%s's pattern takes %q, outside %s to %s", each.schema, each.property, text, least, most)
			}
			if held && !taken {
				t.Errorf("%s.%s's pattern refuses %q, from %s to %s", each.schema, each.property, text, least, most)
			}
		}
	}
}
