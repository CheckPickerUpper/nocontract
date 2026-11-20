import { type DateTime, Effect } from "effect"
import { HttpApiClient } from "effect/http-api"
import { ShopApi } from "./shop_openapi_client.generated.mjs"

const shopClient = HttpApiClient.make(ShopApi, { baseUrl: "https://shop.example" })

export const readCadBalance = Effect.gen(function*() {
  const shop = yield* shopClient
  const read = yield* shop.balance.readBalance({ params: { shopper_id: 7n }, query: { currency: "CAD" } })
  const cents: bigint = read.balance.cents
  const changed: DateTime.Utc = read.changed
  return { cents, changed }
}).pipe(Effect.catchTag("ShopperNotFound", ({ code }) => Effect.succeed<"SH-1001">(code)))

type Shop = Effect.Success<typeof shopClient>
type BalanceMethod = "readBalance" | "topUp"
type ReadBalance = Shop["balance"]["readBalance"]
type ReadBalanceFailure = Effect.Error<ReturnType<ReadBalance>>
type ShopperId = Parameters<ReadBalance>[0]["params"]["shopper_id"]
type PlaceOrder = Shop["orders"]["place"]
type CourierNames = keyof Parameters<PlaceOrder>[0]["payload"]["courier_notes"]
type ShopCouriers = "drone" | "post"
type ExpectNever<Unexpected extends never> = Unexpected

export type BalanceUsesGroupLocalNames = ExpectNever<
  Exclude<keyof Shop["balance"], BalanceMethod> | Exclude<BalanceMethod, keyof Shop["balance"]>
>
export type ReadBalanceRefusesTopUpFailure = ExpectNever<Extract<"TopUpRefused", ReadBalanceFailure["_tag"]>>
export type ReadBalanceRefusesStringAndNumberIds = ExpectNever<Extract<"1.5e3" | 7, ShopperId>>
export type PlaceOrderAcceptsExactlyTheShopCouriers = ExpectNever<
  Exclude<CourierNames, ShopCouriers> | Exclude<ShopCouriers, CourierNames>
>
