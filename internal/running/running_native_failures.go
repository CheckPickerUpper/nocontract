package running

import "fmt"

type backendMissing struct{ Directory string }

func (failed backendMissing) Error() string {
	return fmt.Sprintf("%s has no backend language", failed.Directory)
}

type nativeContractMissing struct{ Directory string }

func (failed nativeContractMissing) Error() string {
	return fmt.Sprintf("the compiler produced no HTTP contract for %s", failed.Directory)
}

type nativeSourceHeaderMissing struct {
	Path      string
	FirstLine string
}

func (failed nativeSourceHeaderMissing) Error() string {
	return fmt.Sprintf("%s: generated source needs the NoContract header, found %q", failed.Path, failed.FirstLine)
}
