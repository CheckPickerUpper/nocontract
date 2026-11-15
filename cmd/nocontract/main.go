// Command nocontract writes the code that serves a backend's routes and the Effect HttpApi its
// TypeScript clients call, or checks that the written files are current.
package main

import (
	"context"
	"os"

	"github.com/CheckPickerUpper/nocontract/internal/running"
)

func main() {
	os.Exit(running.Finish(running.Run(running.Request{Arguments: os.Args[1:], Context: context.Background()})))
}
