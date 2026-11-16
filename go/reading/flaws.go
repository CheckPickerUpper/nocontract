package reading

// SignatureFlaw is why a handler is not a func(context.Context, Input) Outcome.
//
//sumtype:decl
type SignatureFlaw interface{ isSignatureFlaw() }

// NotAFunction is a handler whose declaration is not a function.
//
//ergo:not-an-error
type NotAFunction struct{}

// TypeParametersDeclared is a handler, or its receiver, with type parameters.
//
//ergo:not-an-error
type TypeParametersDeclared struct{}

// WrongShape is a handler that does not take two parameters and return one outcome.
//
//ergo:not-an-error
type WrongShape struct{}

// ContextMissing is a handler whose first parameter is not a context.Context.
//
//ergo:not-an-error
type ContextMissing struct{}

// PointerReceiver is a method handler with a pointer receiver.
//
//ergo:not-an-error
type PointerReceiver struct{}

func (NotAFunction) isSignatureFlaw()           {}
func (TypeParametersDeclared) isSignatureFlaw() {}
func (WrongShape) isSignatureFlaw()             {}
func (ContextMissing) isSignatureFlaw()         {}
func (PointerReceiver) isSignatureFlaw()        {}

// ReceiverFlaw is why the routes cannot be served with a method handler's receiver.
//
//sumtype:decl
type ReceiverFlaw interface{ isReceiverFlaw() }

// ReceiverElsewhere is a receiver type declared in another package than the route.
//
//ergo:not-an-error
type ReceiverElsewhere struct{}

// ReceiverUnexported is a receiver type code mounting the routes cannot name.
//
//ergo:not-an-error
type ReceiverUnexported struct{}

func (ReceiverElsewhere) isReceiverFlaw()  {}
func (ReceiverUnexported) isReceiverFlaw() {}

// TypeFlaw is why a field's type has no JSON shape the API can declare.
//
//sumtype:decl
type TypeFlaw interface{ isTypeFlaw() }

// ByteSliceField is a byte slice, which encoding/json writes as base64 text.
//
//ergo:not-an-error
type ByteSliceField struct{}

// MapKeyNotText is a map whose keys are not strings, which is no JSON object.
//
//ergo:not-an-error
type MapKeyNotText struct{}

// PointerField is a pointer, which may be nil.
//
//ergo:not-an-error
type PointerField struct{}

// InterfaceField is an interface, which has no single JSON shape.
//
//ergo:not-an-error
type InterfaceField struct{}

// ShapelessField is a type encoding/json cannot write, such as a channel, function or complex
// number.
//
//ergo:not-an-error
type ShapelessField struct{}

func (ByteSliceField) isTypeFlaw() {}
func (MapKeyNotText) isTypeFlaw()  {}
func (PointerField) isTypeFlaw()   {}
func (InterfaceField) isTypeFlaw() {}
func (ShapelessField) isTypeFlaw() {}
