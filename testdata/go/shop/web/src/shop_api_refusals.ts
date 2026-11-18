import { Effect } from "effect"
import { HttpApiClient } from "effect/http-api"
import { ShopApi } from "./shop_api.generated.js"

// Calls the generated client cannot type-check: each line marked @ts-expect-error must stay an error.
const client = HttpApiClient.make(ShopApi, { baseUrl: "https://shop.example" })

export const catchingAFailureTheRouteNeverAnswers = Effect.gen(function*() {
  const shop = yield* client
  return yield* shop.balance.readBalance({ params: { shopper_id: 7 }, query: { currency: "CAD" } })
  // @ts-expect-error readBalance never answers TopUpRefused
}).pipe(Effect.catchTag("TopUpRefused", () => Effect.succeed(0)))

export const sendingADecimalShopperId = Effect.gen(function*() {
  const shop = yield* client
  // @ts-expect-error shopper_id is a whole number
  return yield* shop.balance.readBalance({ params: { shopper_id: "1.5e3" }, query: { currency: "CAD" } })
})
