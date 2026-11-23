package assembling_test

import (
	"testing"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/google/go-cmp/cmp"
)

func TestHeaderDeclarationsKeepTheirRefusalFacts(t *testing.T) {
	t.Parallel()
	field := description.Field{Name: fieldName(t, "order_count"), Schema: description.Integer{Width: description.Signed64{}}}
	first := assembling.DeclaredHeader{Name: "X-Order-Count", Field: field, Position: at(6)}
	for _, test := range []struct {
		name    string
		headers []assembling.DeclaredHeader
		problem assembling.Problem
		at      assembling.Position
	}{
		{name: "an empty written header", headers: []assembling.DeclaredHeader{{Name: "", Field: field, Position: at(7)}}, problem: description.HeaderNameEmpty{Name: ""}, at: at(7)},
		{name: "a space in a name", headers: []assembling.DeclaredHeader{{Name: "X Count", Field: field, Position: at(7)}}, problem: description.HeaderNameInvalid{Name: "X Count", Character: " ", Offset: 1}, at: at(7)},
		{name: "a non ASCII letter", headers: []assembling.DeclaredHeader{{Name: "X-Café", Field: field, Position: at(7)}}, problem: description.HeaderNameInvalid{Name: "X-Café", Character: "é", Offset: 5}, at: at(7)},
		{name: "a name differing only by case", headers: []assembling.DeclaredHeader{first, {Name: "x-ORDER-count", Field: description.Field{Name: fieldName(t, "second_count"), Schema: field.Schema}, Position: at(7)}}, problem: assembling.HeaderNameRepeated{Name: "x-ORDER-count", Field: "second_count", FirstName: "X-Order-Count", FirstField: "order_count", FirstPosition: at(6).String()}, at: at(7)},
		{name: "one native field reads two headers", headers: []assembling.DeclaredHeader{first, {Name: "X-Count", Field: field, Position: at(7)}}, problem: assembling.HeaderFieldRepeated{Field: "order_count", Name: "X-Count", FirstName: "X-Order-Count", FirstPosition: at(6).String()}, at: at(7)},
		{name: "a floating point header", headers: []assembling.DeclaredHeader{{Name: "X-Order-Count", Field: description.Field{Name: field.Name, Schema: description.Finite{}}, Position: at(7)}}, problem: assembling.ParameterUnsupported{Part: "header", Field: "order_count", Schema: "description.Finite"}, at: at(7)},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := route(t, "readHeaders", "/headers", 2)
			endpoint.Headers = test.headers
			switch assembled := assembling.Assemble(api(t, endpoint)).(type) {
			case assembling.APIRefused:
				if len(assembled.Refusals) != 1 {
					t.Fatalf("refusals = %+v; want one precise header refusal", assembled.Refusals)
				}
				refusal := assembled.Refusals[0]
				if refusal.Position != test.at {
					t.Errorf("position = %+v; want %+v", refusal.Position, test.at)
				}
				if difference := cmp.Diff(test.problem, refusal.Problem); difference != "" {
					t.Errorf("header refusal differs (-want +got):\n%s", difference)
				}
			case assembling.APIAssembled:
				t.Fatalf("invalid headers were accepted: %+v", assembled.API)
			case nil:
				t.Fatal("assembly returned no outcome")
			}
		})
	}
}

func TestReservedHeadersRequireTheirDedicatedFeature(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"aCCept", "CONTENT-type", "Authorization", "cookie"} {
		t.Run(name, func(t *testing.T) {
			endpoint := route(t, "readHeaders", "/headers", 2)
			endpoint.Headers = []assembling.DeclaredHeader{{Name: name, Field: description.Field{Name: fieldName(t, "value"), Schema: description.Text{}}, Position: at(7)}}
			switch assembled := assembling.Assemble(api(t, endpoint)).(type) {
			case assembling.APIRefused:
				if len(assembled.Refusals) != 1 {
					t.Fatalf("refusals = %+v; want one reserved header refusal", assembled.Refusals)
				}
				problem, reserved := assembled.Refusals[0].Problem.(description.HeaderNameReserved)
				if !reserved || problem.Name != name || assembled.Refusals[0].Position != at(7) {
					t.Fatalf("refusal = %+v; want the reserved name as written at its declaration", assembled.Refusals[0])
				}
			case assembling.APIAssembled:
				t.Fatalf("reserved header was accepted: %+v", assembled.API)
			case nil:
				t.Fatal("assembly returned no outcome")
			}
		})
	}
}
