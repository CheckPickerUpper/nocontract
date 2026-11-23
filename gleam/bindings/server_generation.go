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
		return generateServer(project, checked.API, description.CommonRuntimeFailures())
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
				parameters, arguments, lifetime := "", "", "_lifetime"
				invocation := "input |> " + generated.module(route.Module) + "." + route.Function
				switch route.Lifetime.(type) {
				case WithRequestLifetime:
					parameters, arguments, lifetime = ", lifetime: request_deadline.RequestLifetime", ", lifetime", "lifetime"
					invocation = generated.module(route.Module) + "." + route.Function + "(input, lifetime)"
				case WithoutRequestLifetime, nil:
				}
				call := "case " + reader + "(request, parameters) { Ok(input) -> " + invocation + " |> " + generated.outcomeEncoder(route.Output) + " Error(cause) -> request_refused(cause) }"
				if guard, found := project.Guards[operation]; found {
					generated.module("gleam/option")
					generated.module("gleam/list")
					call = "case " + generated.module(guard.Module) + "." + guard.Function + "(" + quoted(guard.Scope) + ", request.headers |> list.reverse |> dict.from_list) { option.Some(refusal) -> " + generated.outcomeEncoder(guard.Output) + "(refusal) option.None -> { " + call + " } }"
				}
				handler := fmt.Sprintf("serve_route_%d", len(readers))
				readers = append(readers, generated.inputReader(reader, route.Input, endpoint)+"\nfn "+handler+"(request: wisp.Request, parameters: dict.Dict(String, String)"+parameters+") -> Answer { "+call+" }\n")
				dispatch = append(dispatch, "target.Selected(operation: "+name+", parameters:) -> #("+quoted(description.PathTemplate(endpoint.Path))+", SelectedRoute(run: fn("+lifetime+") { "+handler+"(request, parameters"+arguments+") }))")
			}
		}
	}
	generated.module("gleam/option")
	main := "type Operation {\n" + strings.Join(operations, "\n") + "\n}\n\n" +
		loggedRequest + "fn select_request(request: wisp.Request) -> #(String, RequestSelection) {\ncase target.match_target([" + strings.Join(routes, ",\n") + "], method: http.method_to_string(request.method), path: request.path) {\n" +
		strings.Join(dispatch, "\n") + "\n" +
		"target.UnknownPath(path:) -> #(request.path, ImmediateAnswer(run: fn() { shared_route_not_found(method: http.method_to_string(request.method), path:) }))\n" +
		"target.UndeclaredMethod(allowed:) -> #(request.path, ImmediateAnswer(run: fn() { refuse_method(method: request.method, allowed:) }))\n" +
		"target.UnreadablePath(segment:) -> #(request.path, ImmediateAnswer(run: fn() { let written = segment |> json.string |> json.to_string shared_request_refused(part: \"path\", name: segment, reason: \"invalid URL escape \" <> written) }))\n}\n}\n\n" + strings.Join(readers, "\n")
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
	imports := []string{"import gleam/dict", "import gleam/http", "import gleam/http/response", "import gleam/json", "import gleam/otp/actor", "import gleam/otp/static_supervisor", "import gleam/string", "import mist", "import nocontract_runtime/bind_address", "import nocontract_runtime/request_deadline", "import nocontract_runtime/request_layers", "import nocontract_runtime/request_logging", "import nocontract_runtime/request_reading as request_input", "import nocontract_runtime/server_options", "import nocontract_runtime/target_matching as target", "import nocontract_runtime/value_reading as wire", "import wisp", "import wisp/wisp_mist"}
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

type RequestSelection {
  SelectedRoute(run: fn(request_deadline.RequestLifetime) -> Answer)
  ImmediateAnswer(run: fn() -> Answer)
}

fn handle_request(request: wisp.Request, layers: List(request_layers.Layer), deadline: option.Option(request_deadline.RequestDeadline)) -> wisp.Response {
  let #(duration_us, #(route, answer)) = request_logging.measure_request(fn() {
    let #(route, selected) = select_request(request)
    let visit = request_layers.Visit(method: http.method_to_string(request.method), path: request.path, route:)
    case selected {
      SelectedRoute(run:) -> #(route, answer_route(route:, deadline:, run: fn(lifetime) {
        answer_through_layers(layers, visit, fn() { run(lifetime) })
      }))
      ImmediateAnswer(run:) -> #(route, answer_through_layers(layers, visit, run))
    }
  })
  request_logging.log_request(request_logging.RequestLog(
    route:,
    method: http.method_to_string(request.method),
    status: answer.response.status,
    code: answer.code,
    duration_us:,
  ))
  answer.response
}

fn answer_through_layers(layers: List(request_layers.Layer), visit: request_layers.Visit, run: fn() -> Answer) -> Answer {
  let finish = request_layers.enter(layers, visit)
  let answer = run()
  finish(request_layers.Answered(status: answer.response.status, code: answer.code))
  answer
}

fn answer_route(route route: String, deadline deadline: option.Option(request_deadline.RequestDeadline), run run: fn(request_deadline.RequestLifetime) -> Answer) -> Answer {
  case deadline {
    option.None -> request_deadline.without_deadline(run:)
    option.Some(within) -> {
      case request_deadline.answer_before_deadline(within:, run:) {
        request_deadline.RequestCompleted(answer:) -> answer
        request_deadline.RequestExpired(timeout_milliseconds:, ..) -> shared_request_deadline_exceeded(route:, timeout_milliseconds:)
        request_deadline.RequestWorkerExited(stopped:) -> request_deadline.worker_exited(stopped)
      }
    }
  }
}

`

const serverStart = `
/// Checks the listener's address and port. Port zero asks the kernel to assign one.
pub fn options(address address: String, port port: Int) -> Result(server_options.StartOptions, server_options.OptionsRefusal) {
  server_options.parse(address:, port:)
}

/// A listener that Mist could not start, with its attempted options and OTP error.
pub type StartRefusal { ListenerNotStarted(options: server_options.StartOptions, cause: actor.StartError) }

/// Starts only the declared routes inside the application's BEAM.
pub fn start(options: server_options.StartOptions) -> Result(actor.Started(static_supervisor.Supervisor), StartRefusal) {
  let configuration = server_options.read_configuration(options)
  case handle_request(_, configuration.layers, configuration.deadline)
    |> wisp_mist.handler(wisp.random_string(64))
    |> mist.new
    |> mist.bind(bind_address.to_string(configuration.address))
    |> mist.port(configuration.port)
    |> mist.start {
    Ok(started) -> Ok(started)
    Error(cause) -> Error(ListenerNotStarted(options:, cause:))
  }
}
`
