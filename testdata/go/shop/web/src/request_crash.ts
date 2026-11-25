import { strict as assert } from "node:assert"
import { Config, Effect } from "effect"
import { FetchHttpClient } from "effect/http"
import { HttpApiClient } from "effect/http-api"
import { RequestCrashed, ShopApi } from "./shop_api.generated.js"

const decoded = Effect.gen(function*() {
  const baseUrl = yield* Config.NonEmptyString("SHOP_URL")
  const shop = yield* HttpApiClient.make(ShopApi, { baseUrl })
  const refusal: RequestCrashed = yield* shop.orders.crash({ params: { cause: "panic" } }).pipe(
    Effect.map((answer) => assert.fail(`The deliberate crash answered ${answer._tag}`)),
    Effect.catchTag("RequestCrashed", Effect.succeed)
  )
  assert.equal(refusal._tag, "RequestCrashed")
  assert.equal(refusal.code, "NC-1172")
  assert.deepEqual(refusal.route, { method: "GET", path: "/crashes/{cause}" })
  return refusal
}).pipe(Effect.provide(FetchHttpClient.layer))

console.log(JSON.stringify(await Effect.runPromise(decoded)))
