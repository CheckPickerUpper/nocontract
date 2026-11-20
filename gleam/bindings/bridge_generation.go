package bindings

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/outcome"
)

// Project is the checked native operations and the folder of their dedicated bridge package.
type Project struct {
	Directory string
	Server    string
	Package   string
	Routes    []Route
	Guards    []Guard
}

// GenerationRefused preserves every unresolved checked value encountered during generation.
type GenerationRefused struct{ Problems []error }

func (failure GenerationRefused) Error() string {
	return fmt.Sprintf("Gleam native generation refused: %v", failure.Problems)
}
func (failure GenerationRefused) Unwrap() []error { return failure.Problems }

// DependencyPathRefused preserves the two package folders and the relative-path failure.
type DependencyPathRefused struct {
	Server      string
	Application string
	Cause       error
}

func (failure DependencyPathRefused) Error() string {
	return fmt.Sprintf("Gleam bridge dependency from %s to %s: %v", failure.Server, failure.Application, failure.Cause)
}
func (failure DependencyPathRefused) Unwrap() error { return failure.Cause }

// Generate emits a separate path-dependent bridge, with typed decoders, direct calls and exhaustive answers.
func Generate(project Project) outcome.Result[[]native.File] {
	return outcome.Fold(outcome.TupleToResult(filepath.Rel(project.Server, project.Directory)), func(relative string) outcome.Result[[]native.File] {
		return generateBridge(project, filepath.ToSlash(relative))
	}, func(cause error) outcome.Result[[]native.File] {
		return outcome.Err[[]native.File](DependencyPathRefused{Server: project.Server, Application: project.Directory, Cause: cause})
	})
}

func generateBridge(project Project, dependency string) outcome.Result[[]native.File] {
	generated := generator{imports: map[string]string{}, decoders: map[string]string{}, encoders: map[string]string{}, outcomes: map[string]string{}, problems: map[string]error{}}
	operations, decoderArms, answerArms := []string{"UnknownOperation(name: String)"}, []string{}, []string{}
	for i, route := range project.Routes {
		operation := fmt.Sprintf("CallRoute%d", i)
		operations = append(operations, operation+"(input: "+generated.typeName(route.Input.Native)+")")
		decoderArms = append(decoderArms, quoted(description.OperationID(route.Group, route.Endpoint))+" -> { use input <- decode.subfield([\"input\"], "+generated.decoder(route.Input)+") decode.success("+operation+"(input:)) }")
		answerArms = append(answerArms, operation+"(input:) -> { input |> "+generated.module(route.Module)+"."+route.Function+" |> "+generated.outcomeEncoder(route.Output)+" }")
	}
	for i, guard := range project.Guards {
		generated.module("gleam/dict")
		generated.module("gleam/option")
		operation := fmt.Sprintf("CheckGuard%d", i)
		operations = append(operations, operation+"(scope: String, headers: dict.Dict(String, String))")
		decoderArms = append(decoderArms, quoted(guard.Operation)+" -> { use scope <- decode.subfield([\"input\", \"scope\"], decode.string) use headers <- decode.subfield([\"input\", \"headers\"], decode.dict(decode.string, decode.string)) decode.success("+operation+"(scope:, headers:)) }")
		answerArms = append(answerArms, operation+"(scope:, headers:) -> { case "+generated.module(guard.Module)+"."+guard.Function+"(scope, headers) { option.Some(answer) -> "+generated.outcomeEncoder(guard.Output)+"(answer) option.None -> json.object([#(\"admitted\", json.object([]))]) } }")
	}
	decoderArms = append(decoderArms, "name -> decode.success(UnknownOperation(name:))")
	answerArms = append(answerArms, "UnknownOperation(name:) -> refused(\"Unknown native operation: \" <> name)")
	if len(generated.problems) > 0 {
		problems := []error{}
		for _, name := range slices.Sorted(maps.Keys(generated.problems)) {
			problems = append(problems, generated.problems[name])
		}
		return outcome.Err[[]native.File](GenerationRefused{Problems: problems})
	}
	imports := []string{"import gleam/dynamic/decode", "import gleam/io", "import gleam/json", "import gleam/string", "import nocontract_bridge_io as bridge_io"}
	for _, module := range slices.Sorted(maps.Keys(generated.imports)) {
		imports = append(imports, "import "+module+" as "+generated.imports[module])
	}
	source := description.GeneratedHeader + strings.Join(imports, "\n") + "\n\ntype Operation {\n" + strings.Join(operations, "\n") + "\n}\n\n" +
		"fn operation_decoder() -> decode.Decoder(Operation) {\nuse operation <- decode.field(\"operation\", decode.string)\ncase operation {\n" + strings.Join(decoderArms, "\n") + "\n}\n}\n\n" +
		"fn answer(operation: Operation) -> json.Json {\ncase operation {\n" + strings.Join(answerArms, "\n") + "\n}\n}\n\n" +
		"fn refused(reason: String) -> json.Json { json.object([#(\"refused\", json.object([#(\"reason\", json.string(reason))]))]) }\n\n" +
		"fn answer_line(line: String) -> Nil {\nlet response = case json.parse(from: line, using: operation_decoder()) { Ok(operation) -> answer(operation) Error(cause) -> refused(\"Native request: \" <> line <> \"; decoder: \" <> string.inspect(cause)) }\nresponse |> json.to_string |> io.println\n}\n\n" +
		"fn read_requests() -> Nil {\ncase bridge_io.read_line() { Ok(bridge_io.Line(text:)) -> { answer_line(text) read_requests() } Ok(bridge_io.EndOfInput) -> Nil Error(cause) -> { cause |> string.inspect |> refused |> json.to_string |> io.println } }\n}\n\n" +
		"/// Serves typed native calls on standard input until EOF.\npub fn main() -> Nil { io.println(\"nocontract-ready\") read_requests() }\n"
	for _, name := range slices.Sorted(maps.Keys(generated.decoders)) {
		source += "\n" + generated.decoders[name]
	}
	for _, name := range slices.Sorted(maps.Keys(generated.encoders)) {
		source += "\n" + generated.encoders[name]
	}
	for _, name := range slices.Sorted(maps.Keys(generated.outcomes)) {
		source += "\n" + generated.outcomes[name]
	}
	manifest := "# " + description.GeneratedNotice + "\nname = \"nocontract_native_bridge\"\nversion = \"1.0.0\"\ntarget = \"erlang\"\n\n[dependencies]\ngleam_stdlib = \">= 1.0.0 and < 2.0.0\"\ngleam_json = \">= 3.1.0 and < 4.0.0\"\n" + project.Package + " = { path = " + strconv.Quote(dependency) + " }\n"
	return outcome.Ok([]native.File{
		{Path: filepath.Join(project.Server, "gleam.toml"), Contents: []byte(manifest)},
		{Path: filepath.Join(project.Server, "src", "nocontract_bridge.gleam"), Contents: []byte(source)},
		{Path: filepath.Join(project.Server, "src", "nocontract_bridge_io.gleam"), Contents: []byte(bridgeIO)},
		{Path: filepath.Join(project.Server, "src", "nocontract_bridge_stdio.erl"), Contents: []byte(bridgeStdio)},
	})
}

const bridgeIO = description.GeneratedHeader + `import gleam/dynamic
import gleam/dynamic/decode

/// The next line read from standard input, or its EOF.
pub type StdinLine {
  Line(text: String)
  EndOfInput
}

/// Why standard input could not be read.
pub type StdinRefusal {
  StdinRefused(reason: String)
  StdinUndecodable(errors: List(decode.DecodeError))
}

@external(erlang, "nocontract_bridge_stdio", "read_line")
fn read_raw_line() -> dynamic.Dynamic

fn line_decoder() -> decode.Decoder(Result(StdinLine, StdinRefusal)) {
  use kind <- decode.field("kind", decode.string)
  case kind {
    "line" -> { use text <- decode.field("text", decode.string) decode.success(Ok(Line(text:))) }
    "eof" -> decode.success(Ok(EndOfInput))
    "refused" -> { use reason <- decode.field("reason", decode.string) decode.success(Error(StdinRefused(reason:))) }
    _ -> decode.failure(Error(StdinRefused(reason: "unknown stdin result: " <> kind)), expected: "line, eof, or refused")
  }
}

/// Reads and decodes one native stdin result at the foreign boundary.
pub fn read_line() -> Result(StdinLine, StdinRefusal) {
  case read_raw_line() |> decode.run(line_decoder()) {
    Ok(read) -> read
    Error(errors) -> Error(StdinUndecodable(errors:))
  }
}
`

const bridgeStdio = "% " + description.GeneratedNotice + "\n" + `-module(nocontract_bridge_stdio).
-export([read_line/0]).

read_line() ->
    case io:get_line(standard_io, "") of
        eof -> #{<<"kind">> => <<"eof">>};
        {error, Reason} -> #{<<"kind">> => <<"refused">>, <<"reason">> => unicode:characters_to_binary(io_lib:format("~p", [Reason]))};
        Line -> #{<<"kind">> => <<"line">>, <<"text">> => unicode:characters_to_binary(Line)}
    end.
`
