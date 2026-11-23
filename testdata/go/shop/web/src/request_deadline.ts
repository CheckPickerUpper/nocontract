import { strict as assert } from "node:assert"
import { Config, Effect } from "effect"
import { FetchHttpClient } from "effect/http"
import { HttpApiClient } from "effect/http-api"
import { RequestDeadlineExceeded, ShopApi } from "./shop_api.generated.js"

const program = Effect.gen(function*() {
  const baseUrl = yield* Config.NonEmptyString("SHOP_URL")
  const shop = yield* HttpApiClient.make(ShopApi, { baseUrl })
  const deadline: RequestDeadlineExceeded = yield* shop.balance.readDelayedBalance({
    params: { shopper_id: 7n }, query: { currency: "CAD", delay_milliseconds: 2000 }
  }).pipe(
    Effect.map((answer) => assert.fail(`The expired delivery answered ${answer._tag}`)),
    Effect.catchTag("RequestDeadlineExceeded", Effect.succeed)
  )
  assert.equal(deadline._tag, "RequestDeadlineExceeded")
  assert.equal(deadline.code, "NC-1170")
  assert.equal(deadline.route, "/shoppers/{shopper_id}/delayed-balance")
  assert.equal(deadline.timeout_milliseconds, 50)
  return deadline
}).pipe(Effect.provide(FetchHttpClient.layer))

Effect.runPromise(program).then(
  (decoded) => console.log(JSON.stringify(decoded)),
  (failure: unknown) => {
    console.error(String(failure))
    process.exit(1)
  }
)
