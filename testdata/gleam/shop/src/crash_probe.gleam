/// Raises a native tuple bounds error from this shop fixture.
@external(erlang, "nocontract_shop_crash_probe_ffi", "bounds")
fn read_tuple(values: #(Int), at index: Int) -> Int

/// Selects an element past the end of this fixture's tuple.
pub fn bounds() -> Int {
  read_tuple(#(0), at: 2)
}
