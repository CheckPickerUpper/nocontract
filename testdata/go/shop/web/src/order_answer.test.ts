import { strict as assert } from "node:assert"
import { test } from "node:test"
import { Exit, Schema } from "effect"
import { OrderPlaced } from "./shop_api.generated.js"

test("an order answer decodes child items and enforces their quantities", () => {
  const child = { sku: "strainer", quantity: 1, included: [] }
  const item = { sku: "tea", quantity: 2, included: [child] }
  const order = {
    items: [item],
    gift_note: null,
    deliver_between: ["2026-12-01T09:00:00.000Z", "2026-12-02T09:00:00.000Z"],
    wrapped: true,
    tips: { CAD: 2.5 },
    courier_notes: { drone: "the porch" },
  }
  const wire = { _tag: "OrderPlaced", order }
  const answer = Schema.decodeUnknownSync(OrderPlaced)(wire)
  assert.deepEqual(answer.order.items[0]?.included, [child])
  assert.deepEqual(Schema.encodeSync(OrderPlaced)(answer), wire)

  const invalid = { ...item, included: [{ ...child, quantity: 300 }] }
  assert.ok(Exit.isFailure(Schema.decodeUnknownExit(OrderPlaced)({ ...wire, order: { ...order, items: [invalid] } })))
})
