package bindings

import (
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/gleam/compiler"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/outcome"
)

// Project holds the compiler-specific constructors used by the checked HTTP description.
type Project struct {
	Directory string
	Server    string
	Routes    []Route
	Guards    map[string]Guard
	Records   map[compiler.Identity]Record
}

// GenerationRefused preserves every unresolved checked value encountered during generation.
type GenerationRefused struct{ Problems []error }

func (failure GenerationRefused) Error() string {
	return fmt.Sprintf("Gleam server generation refused: %v", failure.Problems)
}
func (failure GenerationRefused) Unwrap() []error { return failure.Problems }

// ServerFolderRefused identifies an output folder outside the application's source modules.
//
//ergo:code NC-1144
//ergo:message The Gleam output folder {Server} could not be resolved relative to {Source}.
type ServerFolderRefused struct {
	Source string
	Server string
	Cause  error
}

// ServerFolderOutsideSource is an output that cannot be imported as an application module.
//
//ergo:code NC-1143
//ergo:message The Gleam output folder {Server} is outside {Source}.
type ServerFolderOutsideSource struct {
	Source   string
	Server   string
	Relative string
}

func (failure ServerFolderOutsideSource) Error() string {
	return fmt.Sprintf("Gleam server folder %s is outside %s (relative path %s)", failure.Server, failure.Source, failure.Relative)
}

func (failure ServerFolderRefused) Error() string {
	return fmt.Sprintf("Gleam server folder %s must be under %s: %v", failure.Server, failure.Source, failure.Cause)
}
func (failure ServerFolderRefused) Unwrap() error { return failure.Cause }

// ServerModuleNameRefused identifies a directory Gleam cannot import as a module.
//
//ergo:code NC-1152
//ergo:message The Gleam output folder {Server} has invalid module component {Component}.
type ServerModuleNameRefused struct {
	Source    string
	Server    string
	Relative  string
	Component string
	Index     int
}

func (failure ServerModuleNameRefused) Error() string {
	return fmt.Sprintf("Gleam server folder %s under %s has invalid module component %q at index %d (relative path %s)", failure.Server, failure.Source, failure.Component, failure.Index, failure.Relative)
}

var moduleComponent = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Generate emits the application's typed Wisp/Mist server from its assembled description.
func Generate(project Project, checked assembling.APIAssembled) outcome.Result[[]native.File] {
	source := filepath.Join(project.Directory, "src")
	return outcome.Fold(outcome.TupleToResult(filepath.Rel(source, project.Server)), func(relative string) outcome.Result[[]native.File] {
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return outcome.Err[[]native.File](ServerFolderOutsideSource{Source: source, Server: project.Server, Relative: relative})
		}
		if relative != "." {
			for index, component := range strings.Split(relative, string(filepath.Separator)) {
				if !moduleComponent.MatchString(component) {
					return outcome.Err[[]native.File](ServerModuleNameRefused{Source: source, Server: project.Server, Relative: relative, Component: component, Index: index})
				}
			}
		}
		return generateServer(project, checked.API, description.SharedFailures())
	}, func(cause error) outcome.Result[[]native.File] {
		return outcome.Err[[]native.File](ServerFolderRefused{Source: source, Server: project.Server, Cause: cause})
	})
}

func generateServer(project Project, api description.API, failures []description.SharedFailure) outcome.Result[[]native.File] {
	generated := generator{records: project.Records, members: map[string]description.Member{}, imports: map[string]string{}, decoders: map[string]string{}, encoders: map[string]string{}, outcomes: map[string]string{}, problems: map[string]error{}}
	for _, member := range api.Members {
		generated.members[member.Name] = member
	}
	operations, routes, dispatch, readers := []string{}, []string{}, []string{}, []string{}
	for _, group := range api.Groups {
		for _, endpoint := range group.Endpoints {
			operation := description.OperationID(group.Name, endpoint.Name)
			for _, route := range project.Routes {
				if operation != description.OperationID(route.Group, route.Endpoint) {
					continue
				}
				name := fmt.Sprintf("Route%d", len(operations))
				operations = append(operations, name)
				routes = append(routes, "target.Route(operation: "+name+", method: "+quoted(endpoint.Method.Name())+", path: "+pathSegments(endpoint.Path)+")")
				reader := fmt.Sprintf("read_route_%d", len(readers))
				call := "case " + reader + "(request, parameters) { Ok(input) -> input |> " + generated.module(route.Module) + "." + route.Function + " |> " + generated.outcomeEncoder(route.Output) + " Error(cause) -> request_refused(cause) }"
				if guard, found := project.Guards[operation]; found {
					generated.module("gleam/option")
					generated.module("gleam/list")
					call = "case " + generated.module(guard.Module) + "." + guard.Function + "(" + quoted(guard.Scope) + ", request.headers |> list.reverse |> dict.from_list) { option.Some(refusal) -> " + generated.outcomeEncoder(guard.Output) + "(refusal) option.None -> { " + call + " } }"
				}
				handler := fmt.Sprintf("serve_route_%d", len(readers))
				readers = append(readers, generated.inputReader(reader, route.Input, endpoint)+"\nfn "+handler+"(request: wisp.Request, parameters: dict.Dict(String, String)) -> Answer { "+call+" }\n")
				dispatch = append(dispatch, "target.Selected(operation: "+name+", parameters:) -> #("+quoted(description.PathTemplate(endpoint.Path))+", "+handler+"(request, parameters))")
			}
		}
	}
	generated.module("gleam/option")
	main := "type Operation {\n" + strings.Join(operations, "\n") + "\n}\n\n" +
		loggedRequest + "fn answer_request(request: wisp.Request) -> #(String, Answer) {\ncase target.match_target([" + strings.Join(routes, ",\n") + "], method: http.method_to_string(request.method), path: request.path) {\n" +
		strings.Join(dispatch, "\n") + "\n" +
		"target.UnknownPath(path:) -> #(request.path, shared_route_not_found(method: http.method_to_string(request.method), path:))\n" +
		"target.UndeclaredMethod(allowed:) -> #(request.path, refuse_method(method: request.method, allowed:))\n" +
		"target.UnreadablePath(segment:) -> { let written = segment |> json.string |> json.to_string #(request.path, shared_request_refused(part: \"path\", name: segment, reason: \"invalid URL escape \" <> written)) }\n}\n}\n\n" + strings.Join(readers, "\n")
	main += queryTextFunction(api)
	main += generated.recordSchemas(api.Structs) + sharedResponses(generated, failures) + serverStart
	for _, definitions := range []map[string]string{generated.decoders, generated.encoders, generated.outcomes} {
		for _, name := range slices.Sorted(maps.Keys(definitions)) {
			main += "\n" + definitions[name]
		}
	}
	if len(generated.problems) > 0 {
		problems := []error{}
		for _, name := range slices.Sorted(maps.Keys(generated.problems)) {
			problems = append(problems, generated.problems[name])
		}
		return outcome.Err[[]native.File](GenerationRefused{Problems: problems})
	}
	imports := []string{"import gleam/dict", "import gleam/http", "import gleam/http/response", "import gleam/json", "import gleam/otp/actor", "import gleam/otp/static_supervisor", "import gleam/string", "import mist", "import nocontract_runtime/bind_address", "import nocontract_runtime/request_logging", "import nocontract_runtime/request_reading as request_input", "import nocontract_runtime/target_matching as target", "import nocontract_runtime/value_reading as wire", "import wisp", "import wisp/wisp_mist"}
	if len(generated.decoders) > 0 {
		imports = append(imports, "import gleam/dynamic/decode")
	}
	for _, module := range slices.Sorted(maps.Keys(generated.imports)) {
		if module != "gleam/dict" {
			imports = append(imports, "import "+module+" as "+generated.imports[module])
		}
	}
	return outcome.Ok([]native.File{{Path: filepath.Join(project.Server, "server.gleam"), Contents: []byte(description.GeneratedHeader + strings.Join(imports, "\n") + "\n\n" + main)}})
}

const loggedRequest = `
type Answer {
  Answer(response: wisp.Response, code: option.Option(String))
}

fn handle_request(request: wisp.Request) -> wisp.Response {
  let #(duration_us, #(route, answer)) = request_logging.measure_request(fn() { answer_request(request) })
  request_logging.log_request(request_logging.RequestLog(
    route:,
    method: http.method_to_string(request.method),
    status: answer.response.status,
    code: answer.code,
    duration_us:,
  ))
  answer.response
}

`

const serverStart = `
/// A listening address and port, accepted only by options.
pub opaque type StartOptions { StartOptions(address: bind_address.BindAddress, port: Int) }

/// Why an address or port cannot start a listener.
pub type OptionsRefusal {
  EmptyAddress(address: String)
  AddressRefused(cause: bind_address.AddressRefusal)
  PortOutside(port: Int, least: Int, most: Int)
}

/// Checks the listener's address and port. Port zero asks the kernel to assign one.
pub fn options(address address: String, port port: Int) -> Result(StartOptions, OptionsRefusal) {
  case address, port {
    "", _ -> Error(EmptyAddress(address:))
    _, port if port < 0 || port > 65_535 -> Error(PortOutside(port:, least: 0, most: 65_535))
    _, _ -> {
      case bind_address.parse(address) {
        Ok(address) -> Ok(StartOptions(address:, port:))
        Error(cause) -> Error(AddressRefused(cause:))
      }
    }
  }
}

/// A listener that Mist could not start, with its attempted options and OTP error.
pub type StartRefusal { ListenerNotStarted(options: StartOptions, cause: actor.StartError) }

/// Starts only the declared routes inside the application's BEAM.
pub fn start(options: StartOptions) -> Result(actor.Started(static_supervisor.Supervisor), StartRefusal) {
  case handle_request
    |> wisp_mist.handler(wisp.random_string(64))
    |> mist.new
    |> mist.bind(bind_address.to_string(options.address))
    |> mist.port(options.port)
    |> mist.start {
    Ok(started) -> Ok(started)
    Error(cause) -> Error(ListenerNotStarted(options:, cause:))
  }
}
`
