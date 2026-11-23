import { strict as assert } from "node:assert"
import { Config, DateTime, Effect } from "effect"
import { FetchHttpClient, HttpClient, HttpClientRequest, HttpClientResponse } from "effect/http"
import { HttpApiClient } from "effect/http-api"
import { MethodNotDeclared, NotSignedIn, ShopApi, ShopperCredentialRejected, ShopperNotFound } from "./shop_api.generated.js"

const program = Effect.gen(function*() {
  const baseUrl = yield* Config.NonEmptyString("SHOP_URL")
  const shopper = yield* HttpApiClient.make(ShopApi, {
    baseUrl,
    transformClient: HttpClient.mapRequest(HttpClientRequest.setHeader("X-Shopper", "shopper-7"))
  })
  const read = yield* shopper.balance.readBalance({ params: { shopper_id: 7n }, query: { currency: "CAD" } })
  const changed: DateTime.Utc = read.changed
  assert.equal(read.balance.cents, 1250n)
  assert.equal(read.balance.currency, "CAD")
  assert.equal(DateTime.formatIso(changed), "2026-11-14T12:00:00.000Z")

  const missing: ShopperNotFound = yield* shopper.balance.readBalance({ params: { shopper_id: 9007199254740993n }, query: { currency: "CAD" } }).pipe(
    Effect.map((answer) => assert.fail(`The missing shopper answered ${answer._tag}`)),
    Effect.catchTag("ShopperNotFound", Effect.succeed)
  )
  assert.equal(missing._tag, "ShopperNotFound")
  assert.equal(missing.code, "SH-1001")
  assert.equal(missing.shopper_id, 9007199254740993n)

  const queued = yield* shopper.balance.topUp({ params: { shopper_id: 7n }, payload: { amount: { cents: 500n, currency: "USD" } } })
  assert.equal(queued.reference, "USD")

  const stranger = yield* HttpApiClient.make(ShopApi, { baseUrl })
  const refused: NotSignedIn = yield* stranger.balance.readBalance({ params: { shopper_id: 7n }, query: { currency: "CAD" } }).pipe(
    Effect.map((answer) => assert.fail(`The unsigned request answered ${answer._tag}`)),
    Effect.catchTag("NotSignedIn", Effect.succeed)
  )
  assert.equal(refused._tag, "NotSignedIn")
  assert.equal(refused.code, "SH-1101")
  assert.equal(refused.scope, "read-balance")

  const unverified = yield* HttpApiClient.make(ShopApi, {
    baseUrl,
    transformClient: HttpClient.mapRequest(HttpClientRequest.setHeader("X-Shopper", "shopper-8"))
  })
  const rejected: ShopperCredentialRejected = yield* unverified.balance.readBalance({ params: { shopper_id: 7n }, query: { currency: "CAD" } }).pipe(
    Effect.map((answer) => assert.fail(`The unverified shopper answered ${answer._tag}`)),
    Effect.catchTag("ShopperCredentialRejected", Effect.succeed)
  )
  assert.equal(rejected._tag, "ShopperCredentialRejected")
  assert.equal(rejected.code, "SH-1102")
  assert.equal(rejected.scope, "read-balance")

  const response = yield* HttpClient.del(`${baseUrl}/shoppers/7/balance`)
  assert.equal(response.status, 405)
  assert.equal(response.headers["allow"], "GET")
  const method: MethodNotDeclared = yield* HttpClientResponse.schemaBodyJson(MethodNotDeclared)(response)
  assert.equal(method._tag, "MethodNotDeclared")
  assert.equal(method.code, "NC-1070")
  assert.equal(method.method, "DELETE")
  assert.deepEqual(method.allowed, ["GET"])

  return { read, missing, queued, refused, rejected, method }
}).pipe(Effect.provide(FetchHttpClient.layer))

// JSON.stringify refuses a bigint, so each one is printed as its decimal text followed by n.
const printed = (_key: string, value: unknown): unknown => {
  if (typeof value === "bigint") {
    return `${value}n`
  }
  return value
}

Effect.runPromise(program).then(
  (decoded) => console.log(JSON.stringify(decoded, printed)),
  (failure: unknown) => {
    console.error(String(failure))
    process.exit(1)
  }
)
