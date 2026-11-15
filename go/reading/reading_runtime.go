package reading

import (
	"path/filepath"

	"github.com/samber/mo"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// RuntimeReading is what reading nocontract's runtime package ended as.
//
//sumtype:decl
type RuntimeReading interface{ isRuntimeReading() }

// RuntimeRead is the runtime's generated code: the function answering each failure it answers on
// a route's behalf, with the status its directive declares and the code ErGo gives it.
//
//ergo:not-an-error
type RuntimeRead struct{ Package serverplan.Package }

// RuntimeRefused is every declaration of the runtime's shared failures that could not be read, in
// source order.
//
//ergo:code NC-1057
type RuntimeRefused struct{ Refusals []Refusal }

func (RuntimeRead) isRuntimeReading()    {}
func (RuntimeRefused) isRuntimeReading() {}

// ReadRuntime reads the runtime package's sealed SharedFailure out of the loaded packages, which
// must hold the runtime, as the runtime's own generated code.
func ReadRuntime(loaded []*packages.Package) RuntimeReading {
	index := indexOf(Request{Packages: loaded, APIName: ""})
	shared := readShared(index)
	if refusals := sortedRefusals(shared.found); len(refusals) > 0 {
		return RuntimeRefused{Refusals: refusals}
	}
	return mo.Fold(shared.value, func(sealed sealedRead) RuntimeReading {
		return RuntimeRead{Package: serverplan.Package{
			Name: sealed.declared.loaded.Name, Path: serverplan.NoContractPath,
			Directory: filepath.Dir(index.position(sealed.declared.spec.Pos()).Filename), Serves: serverplan.ServesNoRoutes{},
			Answers: []serverplan.SealedAnswer{{Use: serverplan.RuntimeAnswers{}, Outcome: goName(sealed.declared), Members: plansOfMembers(sealed.members)}},
		}}
	}, func(error) RuntimeReading { return RuntimeRefused{Refusals: []Refusal{}} })
}
