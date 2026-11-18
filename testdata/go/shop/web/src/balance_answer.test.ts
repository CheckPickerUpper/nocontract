import { strict as assert } from "node:assert"
import { test } from "node:test"
import { Exit, Schema } from "effect"
import { BalanceRead, Money } from "./shop_api.generated.js"

// The JSON the shop answers a balance read with, its cents written as the text given.
const answered = (cents: string): unknown =>
  JSON.parse(
    `{"_tag":"BalanceRead","balance":{"cents":"${cents}","currency":"CAD"},"overdraft":null,"changed":"2026-11-14T12:00:00Z","note":"first top-up"}`
  )

// 2^53 + 1 is the least whole number a JavaScript number cannot hold.
test("a balance of 2^53 + 1 cents decodes whole, as a bigint", () => {
  assert.equal(Schema.decodeUnknownSync(BalanceRead)(answered("9007199254740993")).balance.cents, 9007199254740993n)
})

test("a balance past what 64 bits hold is refused", () => {
  assert.ok(Exit.isFailure(Schema.decodeUnknownExit(BalanceRead)(answered("9223372036854775808"))))
})

test("an amount of 2^53 + 1 cents encodes as its decimal text", () => {
  assert.deepEqual(Schema.encodeSync(Money)({ cents: 9007199254740993n, currency: "CAD" }), { cents: "9007199254740993", currency: "CAD" })
})
