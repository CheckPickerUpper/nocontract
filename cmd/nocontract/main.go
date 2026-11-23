// Command nocontract writes the code that serves a backend's routes and the Effect HttpApi its
// TypeScript clients call, checks the generated files, and starts their application commands.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/CheckPickerUpper/nocontract/internal/running"
)

func main() {
	lifetime, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	result := running.Run(running.Request{Arguments: os.Args[1:], Context: lifetime})
	stop()
	os.Exit(running.Finish(result))
}
