import { Effect } from "effect"
import { HttpApiClient } from "effect/http-api"
import { type BalanceRead, ShopApi, type ShopperNotFound, type TopUpQueued } from "./shop_api.generated.js"

// The generated API gives a client whose results and failures are the Go outcome members.
export const readCadBalance = (shopperId: number) =>
  Effect.gen(function*() {
    const client = yield* HttpApiClient.make(ShopApi, { baseUrl: "https://shop.example" })
    const read: BalanceRead = yield* client.balance.readBalance({ params: { shopper_id: shopperId }, query: { currency: "CAD" } })
    return read.balance.cents
  }).pipe(
    Effect.catchTag("ShopperNotFound", (missing: ShopperNotFound) => Effect.succeed(-missing.shopper_id))
  )

export const topUp = (shopperId: number) =>
  Effect.gen(function*() {
    const client = yield* HttpApiClient.make(ShopApi, { baseUrl: "https://shop.example" })
    const queued: TopUpQueued = yield* client.balance.topUp({
      params: { shopper_id: shopperId },
      payload: { amount: { cents: 500, currency: "USD" } }
    })
    return queued.reference
  })
