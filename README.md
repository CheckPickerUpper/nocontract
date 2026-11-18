# NoContract

Your frontend's API client, generated from your backend's handlers, with every error each route
can return. Nobody writes an OpenAPI file or a list of responses by hand: NoContract writes the
OpenAPI file from the same handlers, for every other language's client and for documentation tools.

## The problem it solves

In most setups the errors a route can return are written down twice: once in the handler, and
once in a contract beside it. Nothing checks the two against each other. A handler that starts
answering 409 still compiles, the client still believes the route can only fail with 404, and the
first person to find out is a user.

| Approach | Where a route's errors are listed | What keeps that list true |
| --- | --- | --- |
| OpenAPI file with generators (ogen, openapi-typescript) | the contract file, edited by hand | generating both sides from the file |
| [utoipa](https://github.com/juhaku/utoipa) (Rust), [huma](https://github.com/danielgtaylor/huma) (Go) | an annotation or registration beside the handler | nothing; the list is not checked against what the handler returns |
| NoContract | the handler's return type | the compiler |

With NoContract a handler returns a closed type, and each member of that type is one answer the
route can give. NoContract reads that type and writes both the code that serves the route and an
[Effect](https://effect.website) `HttpApi` client for it. Add a failure to the type and it appears
in the route's error type in TypeScript on the next `generate`; `check` fails in CI until you run
it.

## What you get

- Each failure is its own tagged error in the Effect client, with its fields.
  `Effect.catchTag("ShopperNotFound", ...)` on a route that cannot return `ShopperNotFound` is a
  type error.
- Every failure carries a permanent code such as `SH-1001`, declared once with
  [ErGo](https://github.com/CheckPickerUpper/ErGo) and sent in every response. A log line or a
  user's screenshot names the exact failure after its wording has changed.
- The generated route code writes only the members of the handler's type, each with its declared
  HTTP status. The server answers only the routes the client and the OpenAPI file list, and a
  route behind a guard cannot be served without that guard.
- Go, Odin and Gleam backends are read into the same description and checked by the same rules,
  so they produce the same client and the same JSON.

## Install

In the Go module that serves the routes:

```sh
go get -tool github.com/CheckPickerUpper/nocontract/cmd/nocontract
```

The frontend needs `effect` 4.

## Declaring a route in Go

A route is a function taking a `context.Context` and an input, returning a sealed outcome. Its
`//nocontract:route` line names the method and the path:

```go
// ReadBalance reads one shopper's balance.
//
//nocontract:route GET /shoppers/{shopper_id}/balance
func ReadBalance(ctx context.Context, input ReadInput) ReadOutcome {
	// ...
}
```

The input is a struct. A field tagged `path:"name"` reads that path segment, a field tagged
`query:"name"` reads that query parameter, and a field named `Body` reads the JSON body. A route
that reads nothing takes `nocontract.NoInput`.

```go
// ReadInput names the shopper and the currency to read the balance in.
type ReadInput struct {
	ShopperID int      `path:"shopper_id"`
	Currency  Currency `query:"currency"`
}
```

The outcome is an interface sealed by an unexported method and marked `//sumtype:decl`. Every
type in its package that implements it is one of its members. Each member declares its status
with `//nocontract:status`, and its [ErGo](https://github.com/CheckPickerUpper/ErGo) decision:
a failure carries a permanent `//ergo:code`, a success is `//ergo:not-an-error`.

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

A handler may be a method of a type its package declares, so it can hold what the route needs,
such as a database:

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

A guard is a check in front of the routes that name it, such as a sign-in check. Its refusal is a
sealed type marked `//nocontract:guard` with the guard's name, whose members are failures:

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

A route names its guard and the one argument the guard is handed:

```go
//nocontract:route GET /shoppers/{shopper_id}/balance
//nocontract:guard Shopper read-balance
func (ledger Ledger) ReadBalance(ctx context.Context, input ReadInput) ReadOutcome {
```

The guard implements `nocontract.Guard[refusal.Refusal]`, typed by its own refusal. It decides
and never answers: `CheckRoute` is asked once for each route when the server is built, and
returns how the guard decides each request to that route, or why it cannot check the route at all.
Each request's decision is `nocontract.Admitted` or `nocontract.Refused` holding one member of
the guard's refusal. The generated code writes the refusal with its status and code, or runs the
route. A decision typed by one guard's refusal cannot hold another guard's refusal or a route's
failure.

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

Every route behind a guard lists the guard's refusals among its failures in the TypeScript client.

## Generating

A `nocontract.json` beside `go.mod` names the backend's language, the packages holding routes,
the TypeScript file to write, the name of the API class, the OpenAPI 3.1 file to write with the
version of the API it declares, and the folder of the server package to write. Each path is
relative to `nocontract.json`, and every field is required:

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

```sh
go tool nocontract generate   # writes the files
go tool nocontract check      # exits 1 when a file differs from what generate writes
```

`generate` writes `nocontract_routes.go` into each package that declares a route or a guard's
refusal, `nocontract_server.go` into the server folder, the TypeScript file and the OpenAPI file.
It also writes `nocontract_files_test.go` into the first package that declares a route or a
guard's refusal, in folder order, so `go test ./...` fails while any generated file differs from
what `generate` writes, listing each one. `check` runs the same comparison on its own.

The OpenAPI file lists each route's path and query parameters, its body, and every member it
answers with under that member's status. Each member is its own schema, whose `_tag` and, for a
failure, `code` each allow one value. A whole number is `integer`, and a `time.Time` is a string
of format `date-time`.

## Serving

The server package declares `API`, the only value that serves the routes, and `New`, the only
function that builds one. `New` takes the layers, then one parameter for each handler's receiver
type and each guard, by route package folder and then in the order the package's routes first
need them, each named for its package and its type or guard. A route cannot be served without its
guard or its receiver.

```go
built := server.New([]nocontract.Layer{logRequest}, balance.Ledger{Store: store}, shopperauth.Check{})
```

`New` answers the API, or why a guard could not check one of its routes. The API is an
`http.Handler`:

```go
mo.Fold(built, func(api server.API) error {
	return (&http.Server{Addr: ":8080", Handler: api, ReadHeaderTimeout: 10 * time.Second}).ListenAndServe()
}, func(cause error) error { return cause })
```

The API answers a request no declared route takes on its own:

- a path no route declares: 404 with `RouteNotFound` (`NC-1069`), naming the method and path;
- an `OPTIONS` request to a declared path: 204 with `Allow` listing the path's declared methods;
- any other method on a declared path: 405 with `MethodNotDeclared` (`NC-1070`), naming the
  method and the allowed methods, and `Allow` listing them.

A path segment written as text in one route is preferred over a `{name}` segment in another.

The API is the only handler a project serves. `generate` and `check` refuse, in every file the
project wrote, tests included, each with its file, line and code:

- serving a value whose type is not the API with `http.ListenAndServe`, `ListenAndServeTLS`,
  `Serve`, `ServeTLS`, or `httptest.NewServer`, `NewTLSServer` and `NewUnstartedServer`, or using
  one of them without calling it;
- an `http.Server` written without a `Handler` or with one that is not the API, and setting a
  server's `Handler` to anything else;
- making a handler of its own: `http.HandlerFunc`, a `ServeHTTP` method, or registering on a
  `ServeMux` with `Handle` or `HandleFunc`.

A handler wrapping the API, for logging, metrics or CORS, is refused with the rest. A
[layer](#layers) runs that code around every request instead.

## Layers

A `nocontract.Layer` runs around every request the API answers, matched or not, for logging,
metrics, request ids or CORS headers. It is handed the request as a `nocontract.Visit`, which reads
the method, URL, header and the route matched (`nocontract.RouteMatched` with the declared method
and path, or `nocontract.NoRouteMatched`), and sets headers on the response. Its `next` runs the
route, once however often it is called, and returns the `nocontract.Answered`: the status, the
`_tag` and the code of what the route answered. Only the API builds an `Answered`, and it sends the
route's own answer whatever a layer returns, so a layer cannot change the status or body or answer
in the route's place. Layers run in the order given, the first outermost.

```go
func logRequest(visit nocontract.Visit, next func() nocontract.Answered) nocontract.Answered {
	answered := next()
	fmt.Fprintf(os.Stderr, "%s %s %d %s\n", visit.Method(), visit.URL().Path, answered.Status(), answered.Code().OrElse(""))
	return answered
}
```

## Calling

```ts
import { Effect } from "effect"
import { HttpApiClient } from "effect/http-api"
import { ShopApi } from "./shop_api.generated.js"

const cents = Effect.gen(function*() {
  const client = yield* HttpApiClient.make(ShopApi, { baseUrl: "https://shop.example" })
  const read = yield* client.balance.readBalance({ params: { shopper_id: 7 }, query: { currency: "CAD" } })
  return read.cents
}).pipe(Effect.catchTag("ShopperNotFound", (missing) => Effect.succeed(0)))
```

The client's methods are grouped by Go package and named for each handler. A failure the route
cannot give is a type error in `catchTag`, and so is a query value outside the Go constants.

## On the wire

Every answer is one JSON object: `_tag`, the member's Go type name; `code`, a failure's
permanent code; then the member's fields, named as `encoding/json` names them. Its status is the
member's `//nocontract:status`.

```json
{"_tag":"ShopperNotFound","code":"SH-1001","shopper_id":8}
```

Two failures can answer any route. `RequestRefused` (400, `NC-1001`) is a path segment, query
parameter or body the input could not read, before the handler runs, or a path segment that is
not valid escaping. `AnswerNotWritten`
(500, `NC-1002`) is an outcome that could not be written as JSON. `RouteNotFound` and
`MethodNotDeclared` answer requests no declared route takes, as [Serving](#serving) lists.

## Go field types

| Go | TypeScript schema |
| --- | --- |
| `string` | `Schema.String` |
| a string type with constants | `Schema.Literals` of its constants |
| whole numbers | `Schema.Int` |
| `float32`, `float64` | `Schema.Finite` |
| `bool` | `Schema.Boolean` |
| `time.Time` | `Schema.DateTimeUtcFromString` |
| `mo.Option[T]` | `Schema.NullOr` of `T` |
| slices and arrays | `Schema.Array` |
| `map[string]T` | `Schema.Record(Schema.String, T)` |
| named and unnamed structs | `Schema.Struct` |

A path or query field is text, a whole number, a boolean or a string type with constants.

## What is refused

`generate` and `check` refuse a declaration they cannot serve or describe, each with its file,
line and code: a pointer field, a JSON tag option such as `omitempty`, an embedded field, an
unsealed outcome, a member without a status, a failure without a code, a guard no refusal declares,
a path segment no input field reads, and the rest listed in [ERROR_CODES.md](ERROR_CODES.md).

## Versions

NoContract follows semantic versioning. Before 1.0, any minor release may change the generated
files, the wire format or the declarations NoContract reads.

## Licence

MIT. See [LICENSE](LICENSE).
