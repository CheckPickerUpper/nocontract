//// Scalars with the shop's wire widths and timestamps.

/// An integer represented as an unsigned eight-bit JSON number.
pub type UInt8 {
  UInt8(value: Int)
}

/// An integer represented as an unsigned sixteen-bit JSON number.
pub type UInt16 {
  UInt16(value: Int)
}

/// An RFC 3339 timestamp.
pub type Instant {
  Instant(value: String)
}
