package elsewhere

// Tally is a count a route in another package answers with, written as decimal text by a type no
// other package can name.
type Tally struct {
	Count count `json:"count"`
}

// count is a 64-bit count this package does not export.
type count int64
