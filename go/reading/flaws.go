package reading

// SignatureFlaw is why a handler is not a func(context.Context, Input) Outcome.
//
//sumtype:decl
type SignatureFlaw interface {
	isSignatureFlaw()
	explain() string
}

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

func (NotAFunction) explain() string {
	return "it is not a function"
}

func (TypeParametersDeclared) explain() string {
	return "it has type parameters, so it has no single input or outcome"
}

func (WrongShape) explain() string {
	return "it does not take a context and an input and return one outcome"
}

func (ContextMissing) explain() string {
	return "its first parameter is not a context.Context"
}

func (PointerReceiver) explain() string {
	return "its receiver is a pointer; give the method a value receiver"
}

// ReceiverFlaw is why the routes cannot be served with a method handler's receiver.
//
//sumtype:decl
type ReceiverFlaw interface {
	isReceiverFlaw()
	explain() string
}

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

func (ReceiverElsewhere) explain() string {
	return "its type is declared in another package"
}

func (ReceiverUnexported) explain() string {
	return "its type is unexported, so code mounting the routes cannot name it"
}

// TypeFlaw is why a field's type has no JSON shape the API can declare.
//
//sumtype:decl
type TypeFlaw interface {
	isTypeFlaw()
	explain() string
}

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

func (ByteSliceField) explain() string {
	return "encoding/json writes a byte slice as base64 text"
}

func (MapKeyNotText) explain() string {
	return "only a map with string keys is a JSON object"
}

func (PointerField) explain() string {
	return "a pointer may be nil; a value that may be absent is a mo.Option"
}

func (InterfaceField) explain() string {
	return "an interface has no single JSON shape"
}

func (ShapelessField) explain() string {
	return "encoding/json cannot write it"
}
