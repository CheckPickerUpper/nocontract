import { strict as assert } from "node:assert"
import { test } from "node:test"
import { DateTime, Exit, Schema } from "effect"
import { BalanceRead, Money } from "./shop_api.generated.js"

// The JSON the shop answers a balance read with, its cents written as the text given.
const answered = (cents: string): unknown =>
  JSON.parse(
    `{"_tag":"BalanceRead","balance":{"cents":"${cents}","currency":"CAD"},"overdraft":null,"changed":"2026-11-14T12:00:00Z","note":"first top-up"}`
  )

// 2^53 + 1 is the least whole number a JavaScript number cannot hold.
test("a balance round-trips exact cents and its UTC timestamp", () => {
  const input = answered("9007199254740993")
  const balance = Schema.decodeUnknownSync(BalanceRead)(input)
  assert.equal(balance.balance.cents, 9007199254740993n)
  assert.equal(DateTime.formatIso(balance.changed), "2026-11-14T12:00:00.000Z")
  const encoded = Schema.encodeSync(BalanceRead)(balance)
  assert.equal(encoded.balance.cents, "9007199254740993")
  assert.equal(encoded.changed, "2026-11-14T12:00:00.000Z")
})

test("a balance past what 64 bits hold is refused", () => {
  assert.ok(Exit.isFailure(Schema.decodeUnknownExit(BalanceRead)(answered("9223372036854775808"))))
})

// The server and the published pattern refuse each of these, so the client refuses them too.
test("a balance that is not canonical decimal text is refused", () => {
  for (const cents of ["007", "+7", "-0"]) {
    assert.ok(Exit.isFailure(Schema.decodeUnknownExit(BalanceRead)(answered(cents))), `cents of ${cents} decoded`)
  }
})

test("an amount of 2^53 + 1 cents encodes as its decimal text", () => {
  assert.deepEqual(Schema.encodeSync(Money)({ cents: 9007199254740993n, currency: "CAD" }), { cents: "9007199254740993", currency: "CAD" })
})
