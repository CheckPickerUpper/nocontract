package nocontract_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	nocontract "github.com/CheckPickerUpper/nocontract/go"
)

// queried is the query parameter n of a request carrying text as it.
func queried(t *testing.T, text string) nocontract.Text {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?n="+url.QueryEscape(text), http.NoBody)
	return nocontract.Query(request, "n")
}

// refusedQuery is the refusal of the query parameter n for reason.
func refusedQuery(reason string) nocontract.RequestRefused {
	return nocontract.RequestRefused{Part: nocontract.RequestRefusedPartQuery, Name: "n", Reason: reason}
}

// A whole number is read when it fits its field's type and refused when it does not, at either end.
func TestAWholeNumberIsReadOnlyWhenItFitsItsFieldsType(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		text string
		want nocontract.Reading[int8]
	}{
		{text: "127", want: nocontract.Read[int8]{Value: 127}},
		{text: "-128", want: nocontract.Read[int8]{Value: -128}},
		{text: "128", want: refusedQuery(`strconv.ParseInt: parsing "128": value out of range`)},
		{text: "-129", want: refusedQuery(`strconv.ParseInt: parsing "-129": value out of range`)},
	} {
		if got := nocontract.Signed[int8](queried(t, each.text)); got != each.want {
			t.Errorf("%s was read as %#v; want %#v", each.text, got, each.want)
		}
	}
	for _, each := range []struct {
		text string
		want nocontract.Reading[uint8]
	}{
		{text: "255", want: nocontract.Read[uint8]{Value: 255}},
		{text: "256", want: refusedQuery(`strconv.ParseUint: parsing "256": value out of range`)},
		{text: "-1", want: refusedQuery(`strconv.ParseUint: parsing "-1": invalid syntax`)},
	} {
		if got := nocontract.Unsigned[uint8](queried(t, each.text)); got != each.want {
			t.Errorf("%s was read as %#v; want %#v", each.text, got, each.want)
		}
	}
}

// currency is a field's set of string constants.
type currency string

// Text is read as its field's own string type when it is one of the values the field accepts.
func TestALiteralIsReadAsItsFieldsTypeOnlyWhenItIsOneOfItsValues(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		text string
		want nocontract.Reading[currency]
	}{
		{text: "CAD", want: nocontract.Read[currency]{Value: "CAD"}},
		{text: "cad", want: refusedQuery(`"cad" is not one of the values this field accepts.`)},
	} {
		if got := nocontract.Literal[currency](queried(t, each.text), "CAD", "USD"); got != each.want {
			t.Errorf("%s was read as %#v; want %#v", each.text, got, each.want)
		}
	}
}
