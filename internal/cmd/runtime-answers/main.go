// Command runtime-answers writes nocontract's runtime package's own generated file, which answers
// each failure the runtime answers on a route's behalf with the status its directive declares, or
// checks that the file is current. go generate runs it from the runtime's folder.
package main

import (
	"context"
	"os"

	"github.com/CheckPickerUpper/nocontract/internal/running"
)

func main() {
	os.Exit(running.Finish(running.RunRuntime(running.Request{Arguments: os.Args[1:], Context: context.Background()})))
}
