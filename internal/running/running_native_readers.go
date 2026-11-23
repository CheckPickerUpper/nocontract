package running

import (
	"context"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/gleam"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/nocontract/internal/projectfile"
	"github.com/CheckPickerUpper/nocontract/odin"
)

func readNative(context context.Context, backend projectfile.Backend, request native.Request) outcome.Result[native.Project] {
	switch backend := backend.(type) {
	case projectfile.OdinBackend:
		request.OdinCollections = backend.Collections()
		return odin.Read(context, request)
	case projectfile.GleamBackend:
		return gleam.Read(context, request)
	case projectfile.GoBackend:
		return outcome.Err[native.Project](goServerNeedsMain{Directory: request.Directory})
	case nil:
	}
	return outcome.Err[native.Project](backendMissing{Directory: request.Directory})
}
