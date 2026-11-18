import { Effect } from "effect"
import { FetchHttpClient, HttpClient, HttpClientRequest } from "effect/http"
import { HttpApiClient } from "effect/http-api"
import { ShopApi } from "./shop_api.generated.js"

const baseUrl = process.env["SHOP_URL"] ?? ""

// A client sending as shopper 7, and one sending as no shopper.
const asShopper = HttpApiClient.make(ShopApi, {
  baseUrl,
  transformClient: HttpClient.mapRequest(HttpClientRequest.setHeader("X-Shopper", "7"))
})
const anonymous = HttpApiClient.make(ShopApi, { baseUrl })

// Calls the running Go shop through the generated API and prints what each call decoded to.
const program = Effect.gen(function*() {
  const shopper = yield* asShopper
  const read = yield* shopper.balance.readBalance({ params: { shopper_id: 7n }, query: { currency: "CAD" } })
  const missing = yield* Effect.flip(shopper.balance.readBalance({ params: { shopper_id: 8n }, query: { currency: "CAD" } }))
  const queued = yield* shopper.balance.topUp({ params: { shopper_id: 7n }, payload: { amount: { cents: 500n, currency: "USD" } } })
  const stranger = yield* anonymous
  const refused = yield* Effect.flip(stranger.balance.readBalance({ params: { shopper_id: 7n }, query: { currency: "CAD" } }))
  return { read, missing, queued, refused }
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
