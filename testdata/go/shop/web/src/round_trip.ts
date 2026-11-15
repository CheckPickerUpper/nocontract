import { Effect } from "effect"
import { FetchHttpClient } from "effect/http"
import { HttpApiClient } from "effect/http-api"
import { ShopApi } from "./shop_api.generated.js"

// Calls the running Go shop through the generated API and prints what each call decoded to.
const program = Effect.gen(function*() {
  const client = yield* HttpApiClient.make(ShopApi, { baseUrl: process.env["SHOP_URL"] ?? "" })
  const read = yield* client.balance.readBalance({ params: { shopper_id: 7 }, query: { currency: "CAD" } })
  const missing = yield* Effect.flip(client.balance.readBalance({ params: { shopper_id: 8 }, query: { currency: "CAD" } }))
  const queued = yield* client.balance.topUp({ params: { shopper_id: 7 }, payload: { amount: { cents: 500, currency: "USD" } } })
  return { read, missing, queued }
}).pipe(Effect.provide(FetchHttpClient.layer))

Effect.runPromise(program).then(
  (decoded) => console.log(JSON.stringify(decoded)),
  (failure: unknown) => {
    console.error(String(failure))
    process.exit(1)
  }
)
