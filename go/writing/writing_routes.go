// Package writing writes the Go files NoContract generates. Each package declaring routes gets one
// function per route that reads its input and runs its handler, one exhaustive switch per outcome
// that answers each member with its tag, status and code, and the exported function each guard's
// refusal is answered through. The server package gets the only type that serves the routes and
// the only function that builds it.
package writing

import (
	"fmt"
	"go/format"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/go/serverplan"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

// Render is the formatted Go file of the package.
func Render(served serverplan.Package) outcome.Result[[]byte] {
	names := newImports(served.Path, routeFileImports(served.Serves), []string{})
	body := &strings.Builder{}
	switch serves := served.Serves.(type) {
	case serverplan.ServesRoutes:
		body.WriteString(routesText(serves, names))
	case serverplan.ServesNoRoutes, nil:
	}
	for _, sealed := range served.Answers {
		name := serverplan.AnswerPrefix + sealed.Outcome.Name
		switch use := sealed.Use.(type) {
		case serverplan.GuardAnswers:
			fmt.Fprintf(body, "\n// %s answers a refusal of the %s guard with its status, tag and code, in place of the route\n// the guard refused.", name, use.Guard)
		case serverplan.RuntimeAnswers:
			fmt.Fprintf(body, "\n// %s answers each failure nocontract answers itself\n// with its status, tag and code.", name)
		case nil:
		}
		body.WriteString(answerText(answerFunction{name: name, sealed: sealed.Outcome.Name, members: sealed.Members}, names))
	}
	file := description.GeneratedHeader + "\npackage " + served.Name + "\n\n" + names.declaration() + "\n" + body.String()
	return outcome.TupleToResult(format.Source([]byte(file)))
}

// exported is a name with its first letter in upper case, for the generated functions' names.
func exported(name string) string {
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(first)) + name[size:]
}

// routesText is each route's function, every outcome's answer following the first route that
// answers it.
func routesText(served serverplan.ServesRoutes, names *imports) string {
	text := &strings.Builder{}
	unwritten := map[serverplan.GoName]serverplan.OutcomeAnswer{}
	for _, outcome := range served.Outcomes {
		unwritten[outcome.Outcome] = outcome
	}
	for _, route := range served.Routes {
		text.WriteString(routeText(route, names))
		outcome.ForEachOption(keyed.Entry(unwritten, route.Outcome), func(outcome serverplan.OutcomeAnswer) {
			delete(unwritten, route.Outcome)
			text.WriteString(answerText(answerFunction{name: outcomeFunction(outcome.Outcome, names), sealed: names.qualified(outcome.Outcome), members: outcome.Members}, names))
		})
	}
	return text.String()
}

// handlerCall is the call that runs a route's handler on its input.
func handlerCall(handler serverplan.Handler) string {
	switch called := handler.(type) {
	case serverplan.FunctionHandler:
		return called.Name + "(request.Context(), input)"
	case serverplan.MethodHandler:
		return "receiver." + called.Method + "(request.Context(), input)"
	case nil:
	}
	return ""
}

// routeParameters is what a route's function takes: the receiver of its method handler, if it has
// one, and the request.
func routeParameters(handler serverplan.Handler) string {
	switch called := handler.(type) {
	case serverplan.MethodHandler:
		return "receiver " + called.Receiver + ", request *http.Request"
	case serverplan.FunctionHandler, nil:
	}
	return "request *http.Request"
}

func routeText(route serverplan.Route, names *imports) string {
	name := serverplan.RouteFunction(route.Name)
	text := &strings.Builder{}
	fmt.Fprintf(text, "\n// %s answers one request to %s %s. Only the generated server calls it.\nfunc %s(%s) %s {\n\tvar input %s\n",
		name, route.Method.Name(), route.Path.Text(), name, routeParameters(route.Handler), names.runtime("Answer"), names.qualified(route.Input))
	steps := []string{}
	for _, param := range route.Params {
		steps = append(steps, paramStep(param, names.runtime("Path")+"(request, "+strconv.Quote(param.Name)+")", names))
	}
	for _, param := range route.Query {
		steps = append(steps, paramStep(param, names.runtime("Query")+"(request, "+strconv.Quote(param.Name)+")", names))
	}
	switch route.Body.(type) {
	case serverplan.BodyFieldRead:
		decoder := names.qualified(serverplan.GoName{Package: "encoding/json", Name: "Decoder"})
		steps = append(steps, fmt.Sprintf("return %s(%s(request, func(decoder *%s) error { return decoder.Decode(&input.Body) }), func(struct{}) %s {\n",
			names.runtime("Then"), names.runtime("Body"), decoder, names.runtime("Answer")))
	case serverplan.NoBodyRead, nil:
	}
	for _, step := range steps {
		text.WriteString(step)
	}
	fmt.Fprintf(text, "return %s(%s)\n", outcomeFunction(route.Outcome, names), handlerCall(route.Handler))
	text.WriteString(strings.Repeat("})\n", len(steps)))
	text.WriteString("}\n")
	return text.String()
}

// paramStep opens the step that reads one parameter's text as its field's type and sets the field.
func paramStep(param serverplan.Param, text string, names *imports) string {
	goType := names.goType(param.GoType)
	return fmt.Sprintf("return %s(%s, func(read %s) %s {\ninput.%s = read\n", names.runtime("Then"), readCall(param.Text, goType, text, names), goType,
		names.runtime("Answer"), param.GoField)
}

// readCall is the nocontract call reading a parameter's text as goType.
func readCall(kind serverplan.TextKind, goType, text string, names *imports) string {
	reader, arguments := "Textual", []string{text}
	switch read := kind.(type) {
	case serverplan.TextSigned:
		reader = "Signed"
	case serverplan.TextUnsigned:
		reader = "Unsigned"
	case serverplan.TextTruth:
		reader = "Truth"
	case serverplan.TextLiteral:
		reader = "Literal"
		for _, value := range read.Values {
			arguments = append(arguments, strconv.Quote(value))
		}
	case serverplan.TextAsIs, nil:
	}
	return names.runtime(reader) + "[" + goType + "](" + strings.Join(arguments, ", ") + ")"
}

// outcomeFunction is the generated function that answers a route outcome's members.
func outcomeFunction(outcome serverplan.GoName, names *imports) string {
	switch reference := names.reference(outcome.Package).(type) {
	case qualifiedBy:
		return "nocontractAnswer" + exported(reference.alias) + outcome.Name
	case unqualified, nil:
	}
	return "nocontractAnswer" + outcome.Name
}

// answerFunction is one generated function that answers each member of a sealed type: its name,
// the sealed type as the generated file writes it, and the members.
type answerFunction struct {
	name    string
	sealed  string
	members []serverplan.Member
}

// answerText is the function that answers each member of its sealed type with its status, tag and
// code.
func answerText(function answerFunction, names *imports) string {
	text := &strings.Builder{}
	fmt.Fprintf(text, "\nfunc %s(outcome %s) %s {\n\tswitch member := outcome.(type) {\n", function.name, function.sealed, names.runtime("Answer"))
	for _, member := range function.members {
		switch answer := member.Answer.(type) {
		case description.Fails:
			fmt.Fprintf(text, "\tcase %s:\n\t\treturn %s(%s{Status: %d, Tag: %s, Code: %s}, member)\n", names.qualified(member.Go), names.runtime("Failed"),
				names.runtime("Failure"), answer.Status.Code(), strconv.Quote(member.Go.Name), strconv.Quote(answer.Code.String()))
		case description.Succeeds:
			fmt.Fprintf(text, "\tcase %s:\n\t\treturn %s(%s{Status: %d, Tag: %s}, member)\n", names.qualified(member.Go), names.runtime("Succeeded"),
				names.runtime("Success"), answer.Status.Code(), strconv.Quote(member.Go.Name))
		case nil:
		}
	}
	fmt.Fprintf(text, "\tcase nil:\n\t}\n\treturn %s()\n}\n", names.runtime("NoOutcome"))
	return text.String()
}

// imports names every package the generated file refers to, each foreign package by an alias of
// its own that no name declared in the package takes.
type imports struct {
	home     string
	aliases  map[string]string
	order    []string
	declared []string
}

// routeFileImports are the packages a package's generated file imports by the names it uses:
// net/http only when it serves routes.
func routeFileImports(serves serverplan.Routing) [][2]string {
	fixed := [][2]string{{serverplan.NoContractPath, serverplan.NoContractPackage}}
	switch serves.(type) {
	case serverplan.ServesRoutes:
		return append(fixed, [2]string{"net/http", "http"})
	case serverplan.ServesNoRoutes, nil:
	}
	return fixed
}

// newImports names the fixed packages a file of the package at home imports, every one but home
// itself: the runtime's own file refers to its declarations by their bare names. No alias takes a
// name declared, which the package's other files declare.
func newImports(home string, fixedImports [][2]string, declared []string) *imports {
	names := &imports{home: home, aliases: map[string]string{}, order: []string{}, declared: declared}
	for _, fixed := range fixedImports {
		if fixed[0] == home {
			continue
		}
		names.aliases[fixed[0]] = fixed[1]
		names.order = append(names.order, fixed[0])
	}
	return names
}

// reference is how the generated file refers to a declaration of the package at path: by its bare
// name in its own package, and through a unique alias for any other, which the file then imports.
func (names *imports) reference(path string) reference {
	if path == names.home {
		return unqualified{}
	}
	return outcome.FoldOption(keyed.Entry(names.aliases, path), func(alias string) reference { return qualifiedBy{alias: alias} },
		func() reference { return names.imported(path) })
}

// imported is the package at path imported under an alias no other import or local name takes.
func (names *imports) imported(path string) reference {
	base := path[strings.LastIndex(path, "/")+1:]
	cleaned := strings.Map(func(letter rune) rune {
		if unicode.IsLetter(letter) || unicode.IsDigit(letter) {
			return letter
		}
		return '_'
	}, base)
	taken := slices.Concat(slices.Collect(maps.Values(names.aliases)), localNames(), names.declared)
	alias := cleaned
	for suffix := 2; slices.Contains(taken, alias); suffix++ {
		alias = cleaned + strconv.Itoa(suffix)
	}
	names.aliases[path] = alias
	names.order = append(names.order, path)
	return qualifiedBy{alias: alias}
}

// reference is how the generated file refers to a type of a package: by its bare name, or through
// the package's alias.
//
//sumtype:decl
type reference interface{ isReference() }

// unqualified is a type of the file's own package, or a predeclared type, written by its name.
//
//ergo:not-an-error
type unqualified struct{}

// qualifiedBy is a type of another package, written through that package's alias.
//
//ergo:not-an-error
type qualifiedBy struct{ alias string }

func (unqualified) isReference() {}
func (qualifiedBy) isReference() {}

// localNames are the names the generated functions give their parameters and variables, which no
// package alias may take.
func localNames() []string {
	return []string{"input", "read", "request", "writer", "outcome", "member", "receiver", "api", "layers"}
}

// runtime is a declaration of nocontract's runtime as the generated file writes it.
func (names *imports) runtime(name string) string {
	return names.qualified(serverplan.GoName{Package: serverplan.NoContractPath, Name: name})
}

// goType is a Go type as the generated file writes it, each package it names through that
// package's alias.
func (names *imports) goType(written types.Type) string {
	return types.TypeString(written, func(named *types.Package) string {
		switch reference := names.reference(named.Path()).(type) {
		case qualifiedBy:
			return reference.alias
		case unqualified, nil:
		}
		return ""
	})
}

// qualified is a Go name as the generated file writes it.
func (names *imports) qualified(name serverplan.GoName) string {
	switch reference := names.reference(name.Package).(type) {
	case qualifiedBy:
		return reference.alias + "." + name.Name
	case unqualified, nil:
	}
	return name.Name
}

// declaration is the file's import declaration, or nothing when it imports no package.
func (names *imports) declaration() string {
	lines := []string{}
	for _, path := range names.order {
		lines = append(lines, "\t"+names.aliases[path]+" "+strconv.Quote(path))
	}
	if len(lines) == 0 {
		return ""
	}
	return "import (\n" + strings.Join(lines, "\n") + "\n)\n"
}
