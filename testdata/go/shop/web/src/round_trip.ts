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
  const read = yield* shopper.balance.readBalance({ params: { shopper_id: 7 }, query: { currency: "CAD" } })
  const missing = yield* Effect.flip(shopper.balance.readBalance({ params: { shopper_id: 8 }, query: { currency: "CAD" } }))
  const queued = yield* shopper.balance.topUp({ params: { shopper_id: 7 }, payload: { amount: { cents: 500, currency: "USD" } } })
  const stranger = yield* anonymous
  const refused = yield* Effect.flip(stranger.balance.readBalance({ params: { shopper_id: 7 }, query: { currency: "CAD" } }))
  return { read, missing, queued, refused }
}).pipe(Effect.provide(FetchHttpClient.layer))

Effect.runPromise(program).then(
  (decoded) => console.log(JSON.stringify(decoded)),
  (failure: unknown) => {
    console.error(String(failure))
    process.exit(1)
  }
)
