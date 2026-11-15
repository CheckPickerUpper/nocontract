// Command nocontract writes the code that serves a backend's routes and the Effect HttpApi its
// TypeScript clients call, or checks that the written files are current.
package main

import (
	"context"
	"os"

	"github.com/samber/mo"

	"github.com/CheckPickerUpper/nocontract/internal/running"
)

func main() {
	outcome := running.Run(running.Request{Arguments: os.Args[1:], Context: context.Background()})
	stream := os.Stdout
	if outcome.ExitCode() != 0 {
		stream = os.Stderr
	}
	mo.Fold(running.Print(outcome, stream), func(int) struct{} { return struct{}{} }, func(error) struct{} {
		os.Exit(2)
		return struct{}{}
	})
	os.Exit(outcome.ExitCode())
}
