# NoContract

NoContract generates your frontend API client and OpenAPI specification directly from your backend route handlers. You don't write OpenAPI files by hand or maintain separate response lists; your backend types are the single source of truth.

## The problem it solves

In most setups, a route's possible errors are defined twice: once in the handler logic, and once in an external specification or annotation. Nothing keeps them in sync. If a handler starts returning a `409`, the backend compiles, the client still assumes only `404` is possible, and users hit unhandled edge cases in production.

| Approach | Where errors are defined | What enforces accuracy |
| --- | --- | --- |
| **OpenAPI generators** (`ogen`, `openapi-typescript`) | A hand-edited contract file | Generating both client and server from the file |
| **Annotations** (`utoipa` in Rust, `huma` in Go) | Macros or registration calls next to the handler | Nothing; the compiler doesn't check them against return values |
| **NoContract** | The handler's return type | The compiler |

With NoContract, a handler returns a closed sum type where each member represents a specific HTTP response. NoContract reads that type and generates both the server routing code and a typed Effect `HttpApi` client. If you add a new error to a handler's return type, it immediately shows up in the TypeScript client on the next generation, and CI will fail until the generated files are updated.

## What you get

* **Type-safe client errors:** Every failure becomes a distinct tagged error in Effect with its exact payload fields. Calling `Effect.catchTag("ShopperNotFound", ...)` on a route that doesn't return that error causes a TypeScript compile error.
* **Permanent error codes:** Every failure carries a static code (like `SH-1001`) declared via ErGo and included in every JSON response. Logs and user screenshots will always identify the exact error, even if you change the human-readable message later.
* **Strict server guarantees:** Generated routing code only serializes members of the handler's return type with their declared HTTP statuses. The server won't expose undocumented routes, and guarded routes cannot run without their guards.
* **Cross-backend consistency:** Go, Odin, and Gleam backends parse into the same intermediate representation and follow the exact same validation rules, producing identical clients and JSON wire formats.

## Installation

In the Go module serving your routes:

```bash
go get -tool github.com/CheckPickerUpper/nocontract/cmd/nocontract
```

Your frontend requires only the released `effect` v4 package to use the generated client.

## Declaring a route in Go

A route is a function accepting a `context.Context` and an input struct, returning a sealed outcome interface. The `//nocontract:route` directive specifies the HTTP method (`GET`, `POST`, `PUT`, `PATCH`, or `DELETE`) and path. Paths must start with `/`, and segments can be static text or a single `{name}` parameter.

```go
// ReadBalance reads one shopper's balance.
//
//nocontract:route GET /shoppers/{shopper_id}/balance
func ReadBalance(ctx context.Context, input ReadInput) ReadOutcome {
	// ...
}
```

Input structs use tags to bind request data: `path:"name"` for path segments, `query:"name"` for query parameters, and a field named `Body` for the JSON request body. Routes that take no inputs use `nocontract.NoInput`.

```go
// ReadInput names the shopper and the currency to read the balance in.
type ReadInput struct {
	ShopperID int      `path:"shopper_id"`
	Currency  Currency `query:"currency"`
}
```

The return type must be an interface sealed by an unexported method and annotated with `//sumtype:decl`. Every type in the same package implementing this interface is treated as a valid response. Each response type requires a `//nocontract:status` directive and an ErGo directive (`//ergo:code` for failures, `//ergo:not-an-error` for successes).

```go
// ReadOutcome is what reading a balance ended as.
//
//sumtype:decl
type ReadOutcome interface {
	isReadOutcome()
}

// BalanceRead is the shopper's balance.
//
//ergo:not-an-error
//nocontract:status 200
type BalanceRead struct {
	Cents    int      `json:"cents"`
	Currency Currency `json:"currency"`
}

// ShopperNotFound is a shopper the shop holds no balance for.
//
//ergo:code SH-1001
//nocontract:status 404
type ShopperNotFound struct {
	ShopperID int `json:"shopper_id"`
}

func (BalanceRead) isReadOutcome()     {}
func (ShopperNotFound) isReadOutcome() {}
```

Handlers can also be methods on a struct, allowing you to inject dependencies like database connections:

```go
// Ledger reads balances from its store.
type Ledger struct {
	Store Store
}

// ReadBalance reads one shopper's balance.
//
//nocontract:route GET /shoppers/{shopper_id}/balance
func (ledger Ledger) ReadBalance(ctx context.Context, input ReadInput) ReadOutcome {
	// ...
}
```

## Guards

Guards run authentication or validation checks before a route executes. A guard's failure mode is defined as a sealed interface annotated with `//nocontract:guard <Name>`:

```go
// Refusal is how the shopper check refuses a request before its route runs.
//
//sumtype:decl
//nocontract:guard Shopper
type Refusal interface {
	isRefusal()
}

// NotSignedIn is a request carrying no shopper.
//
//ergo:code SH-1101
//nocontract:status 401
type NotSignedIn struct {
	Scope string `json:"scope"`
}

func (NotSignedIn) isRefusal() {}
```

Attach a guard to a route by specifying the guard's name and a single string argument:

```go
//nocontract:route GET /shoppers/{shopper_id}/balance
//nocontract:guard Shopper read-balance
func (ledger Ledger) ReadBalance(ctx context.Context, input ReadInput) ReadOutcome {
```

The guard implementation satisfies `nocontract.Guard[refusal.Refusal]`. When the server initializes, `CheckRoute` is called once per guarded route to validate the configuration and return a request checker function. For each incoming request, that function returns either `nocontract.Admitted` or `nocontract.Refused` containing one of the guard's refusal types. Because the return type is generic over the guard's specific refusal interface, a guard cannot accidentally return another guard's errors or a route outcome.

```go
func (Check) CheckRoute(route nocontract.GuardedRoute) mo.Result[nocontract.RequestCheck[refusal.Refusal]] {
	return mo.Ok[nocontract.RequestCheck[refusal.Refusal]](func(request *http.Request) nocontract.Decision[refusal.Refusal] {
		if request.Header.Get("X-Shopper") == "" {
			return nocontract.Refused[refusal.Refusal]{Refusal: refusal.NotSignedIn{Scope: route.Argument}}
		}
		return nocontract.Admitted[refusal.Refusal]{}
	})
}
```

In the generated TypeScript client, any route using a guard automatically includes that guard's refusal types in its union of possible errors.

## Code generation

Create a `nocontract.json` file next to your `go.mod`. All paths are relative to this config file. Specify the backend language, route packages, API name, OpenAPI output and version, and server output directory. Use `typescript` to generate an Effect client from the same route description as the OpenAPI specification. You can omit it if you publish only OpenAPI. Unknown keys or syntax errors are rejected with exact line and column numbers. The `api` field must be a valid, non-reserved TypeScript identifier.

Odin projects can set `collections` to map import collection names to directories. For example, `"collections": {"credentials": "./packages"}` lets you import `"credentials:verification"` from the `verification` package in that directory.

```json
{
  "language": "go",
  "packages": ["./..."],
  "typescript": "../web/src/shop_api.generated.ts",
  "api": "ShopApi",
  "openapi": { "file": "shop_api.openapi.json", "version": "1.0.0" },
  "server": "server"
}
```

Run the CLI via Go's tool runner:

```bash
go tool nocontract generate   # Writes all generated files
go tool nocontract check      # Exits with status 1 if any generated file is out of date
```

Running `generate` creates:

1. `nocontract_routes.go` in every package containing routes or guard refusals.
2. `nocontract_server.go` in your configured server directory.
3. The TypeScript client file and the OpenAPI 3.1 JSON spec.
4. `nocontract_files_test.go` in the first package, in folder order, that declares a route or a guard refusal. It names stale files and fails `go test ./...` if the generated files drift from your Go code. `check` runs the same comparison directly.

NoContract generates the client without reading frontend dependencies. The [shop's package manifest](testdata/go/shop/web/package.json) and [Bun lockfile](testdata/go/shop/web/bun.lock) show a complete frontend.

Generated source files have a `// Code generated ... DO NOT EDIT.` header or its equivalent. OpenAPI documents have an ownership marker in `info`. Generation refuses to overwrite a file without its marker. If you have an older unmarked OpenAPI document, move it aside before running `generate`.

The generated OpenAPI 3.1 specification lists request parameters and bodies and maps each response member to its HTTP status code, pinning exact literal values for `_tag` and `code`. Integers up to 32 bits map to bounded JSON integers, 64-bit integers (including `int`) map to `int64` or `uint64` decimal strings with a pattern for the exact range, and `time.Time` maps to `date-time` strings.

Named records can contain themselves through fields such as a list of child records. The OpenAPI specification refers to the same named schema for each child.

## Serving the API

The generated server package exports `API`, which owns the checked route binding, `New`, which constructs it, and `Listen`, which starts its listener. `New` accepts your middleware layers, followed by instances of every handler receiver struct and guard required by your routes, ordered by package path and declaration order. If a guard cannot check one of its routes, `New` returns the cause.

```go
built := server.New(
	[]nocontract.Layer{logRequest},
	balance.Ledger{Store: store},
	shopperauth.Check{},
)

started := mo.Fold(built, func(api server.API) outcome.Result[nocontract.ListeningServer] {
	return server.Listen(ctx, nocontract.ListenOn("127.0.0.1:8080"), api)
}, outcome.Err[nocontract.ListeningServer])

stopped := outcome.Fold(started, func(listening nocontract.ListeningServer) outcome.Result[nocontract.ServerStopped] {
	fmt.Println(listening.Address())
	return listening.Wait()
}, outcome.Err[nocontract.ServerStopped])
```

`Listen` takes your application's context and a `nocontract.ListenOptions` value. Use `ListenOn("127.0.0.1:0")` for an ephemeral port and `Address()` to read the bound address. `Wait()` returns the serving task's typed result. Call `Stop(shutdownContext)` from your shutdown path to stop accepting requests and wait for active requests to finish within the context. It returns `ServerStopped` or the original shutdown failure. Concurrent `Wait` and `Stop` calls share the same listener lifetime.

The generated listener automatically handles standard routing edge cases:

* **Unmatched paths:** Responds with `404` and `RouteNotFound` (`NC-1069`), including the requested method and path.
* **`OPTIONS` requests on valid paths:** Responds with `204` and sets the `Allow` header.
* **Unregistered methods on valid paths:** Responds with `405`, sets the `Allow` header, and returns `MethodNotDeclared` (`NC-1070`).
* **Path precedence:** Static path segments always take priority over `{parameter}` segments.

Use NoContract [layers](#layers) for cross-cutting concerns like logging and CORS. The generated listener owns the route table and HTTP request handling.

## Layers

A `nocontract.Layer` wraps every incoming request, whether it matches a route or not. Layers receive a `nocontract.Visit` (providing read access to the method, URL, headers, and matched route metadata, plus write access to response headers) and a `next()` function.

Calling `next()` executes the route once and returns a `nocontract.Answered` struct containing the response status, `_tag`, and error `code`. Layers cannot alter the response status or body; the server always writes the route's actual return value. Layers execute in the order provided, with the first layer on the outside.

```go
func logRequest(visit nocontract.Visit, next func() nocontract.Answered) nocontract.Answered {
	answered := next()
	fmt.Fprintf(os.Stderr, "%s %s %d %s\n", visit.Method(), visit.URL().Path, answered.Status(), answered.Code().OrElse(""))
	return answered
}
```

## Gleam server

The Gleam application starts its generated Wisp/Mist server on its own BEAM. Add `nocontract_runtime` as a Git dependency from this repository's `gleam/runtime` subdirectory and pin it to the same NoContract revision as the Go tool. The [Gleam shop manifest](testdata/gleam/shop/gleam.toml) lists the released libraries it uses.

Set `language` to `gleam`, list your route modules in `packages`, and set `server` to `src/nocontract`. `generate` writes `nocontract/server` there. Both `generate` and `check` read the selected route modules and their source dependencies, even if a change to an outcome makes the generated server stale.

```gleam
import gleam/erlang/process
import nocontract/server

pub fn main() {
  let assert Ok(options) = server.options(address: "127.0.0.1", port: 8080)
  let assert Ok(_) = server.start(options)
  process.sleep_forever()
}
```

`start` takes validated address and port options. The generated module handles routing, request decoding, and typed JSON replies in the Gleam process.

Gleam request body limits wait for Mist to release bounded reads of chunked requests. The fix is on the [Mist `chunked-request-stream` branch](https://github.com/CheckPickerUpper/mist/commit/c67e45e06554da5d21db7c0ad34809aff3bd6e2d).

Gleam graceful shutdown waits for Mist and Glisten to release a listener stop operation that lets active requests finish. The fixes are on the [Mist `graceful-listener` branch](https://github.com/CheckPickerUpper/mist/commit/40b25eaa3f2f4c85d54e72e2f453a4cb834e4236) and the [Glisten `stop-accepting` branch](https://github.com/CheckPickerUpper/glisten/commit/705d4b68797feb2785b89767fbe28680139446bf). [Glisten PR #56](https://github.com/rawhat/glisten/pull/56) tracks the upstream change.

## Calling from TypeScript

```ts
import { Effect } from "effect"
import { HttpApiClient } from "effect/http-api"
import { ShopApi } from "./shop_api.generated.js"

const cents = Effect.gen(function*() {
  const client = yield* HttpApiClient.make(ShopApi, { baseUrl: "https://shop.example" })
  const read = yield* client.balance.readBalance({
    params: { shopper_id: 7n },
    query: { currency: "CAD" },
  })
  return read.balance.cents
}).pipe(
  Effect.catchTag("ShopperNotFound", () => Effect.succeed(0n))
)
```

Client methods are grouped by Go package name and camelCased from the Go handler name. Passing an invalid constant for a query parameter or catching an error tag that the route cannot emit results in a compile-time type error.

## Wire format

Every response is a JSON object containing `_tag` (the Go struct name), `code` (for errors), and the struct's JSON-serialized fields. The HTTP status code matches the struct's `//nocontract:status` annotation.

```json
{"_tag":"ShopperNotFound","code":"SH-1001","shopper_id":"8"}
```

In addition to `RouteNotFound` and `MethodNotDeclared`, any route can return two built-in framework errors:

* `RequestRefused` (`400`, `NC-1001`): Returned before the handler runs if path parameters, query parameters, or the JSON body fail validation or URL unescaping.
* `AnswerNotWritten` (`500`, `NC-1002`): Returned if the handler's outcome fails to marshal to JSON.

## Type mapping

| Go type | TypeScript Effect Schema |
| --- | --- |
| `string` | `Schema.String` |
| Defined `string` type with constants | `Schema.Literals` of those constants |
| Signed/unsigned integers $\le$ 32 bits | `Schema.Int` bounded to the type's min/max |
| 64-bit integers (`int64`, `uint64`, `int`, `uint`) | `Schema.BigIntFromString` bounded to min/max, parsed from canonical decimal strings |
| `float32`, `float64` | `Schema.Finite` |
| `bool` | `Schema.Boolean` |
| `time.Time` | `Schema.DateTimeUtcFromString` |
| `mo.Option[T]` | `Schema.NullOr` of `T` |
| Slices and arrays | `Schema.Array` |
| `map[string]T` | `Schema.Record(Schema.String, T)` |
| Structs (named or inline) | `Schema.Struct` |

Path and query fields must be strings, integers, booleans, or string constant types. Integers in paths and queries must be formatted as canonical decimal text (`7` is valid; `007`, `+7`, and `-0` are rejected).

In JSON bodies, 64-bit integers are transmitted as canonical decimal strings (e.g., `"9007199254740993"`), matching protobuf's JSON mapping to prevent precision loss past JavaScript's `Number.MAX_SAFE_INTEGER` ($2^{53} - 1$). Passing raw JSON numbers or non-canonical strings like `"007"`, `"+7"`, or `"-0"` for 64-bit integer fields will be rejected with `RequestRefused`.

## Validation rules

`generate` and `check` reject unsupported Go constructs with exact file, line, and error codes. Common rejections include:

* Pointer fields or embedded struct fields
* JSON struct tag options like `omitempty`
* Unsealed outcome interfaces, members missing `//nocontract:status`, or errors missing `//ergo:code`
* Referencing an undefined guard
* Path parameters like `{id}` that have no matching `path:"id"` field in the input struct

See [ERROR_CODES.md](ERROR_CODES.md) for the complete list of validation rules.

## Versioning

NoContract follows Semantic Versioning. Prior to `v1.0.0`, minor releases may introduce breaking changes to generated code, wire formats, or directive syntax.
