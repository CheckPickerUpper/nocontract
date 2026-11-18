package reading

// RouteDirectiveWordCount is a //nocontract:route line that is not one method and one path, as it
// is written.
//
//ergo:code NC-1096
//ergo:message The line {Text} is not one method and one path.
type RouteDirectiveWordCount struct{ Text string }

// RouteMethodUnknown is a //nocontract:route line naming a method no route answers.
//
//ergo:code NC-1097
//ergo:message {Method} in the line {Text} is not a method a route answers: GET, POST, PUT, PATCH or DELETE.
type RouteMethodUnknown struct {
	Text   string
	Method string
}

// StatusDirectiveWordCount is a //nocontract:status line that is not one status, as it is written.
//
//ergo:code NC-1098
//ergo:message The line {Text} on {Member} is not one status.
type StatusDirectiveWordCount struct {
	Member string
	Text   string
}

// StatusNotNumber is a //nocontract:status line whose status is not a number.
//
//ergo:code NC-1099
//ergo:message {Status} in the line {Text} on {Member} is not a number.
type StatusNotNumber struct {
	Member string
	Text   string
	Status string
}

// StatusDirectiveRepeated is a member with a second //nocontract:status line, as it is written.
//
//ergo:code NC-1100
//ergo:message {Member} has a second status line, {Text}; a member answers one status.
type StatusDirectiveRepeated struct {
	Member string
	Text   string
}

// GuardDirectiveWordCount is a //nocontract:guard line on a guard's refusal that is not one name,
// as it is written.
//
//ergo:code NC-1101
//ergo:message The line {Text} on a guard's refusal is not one guard name.
type GuardDirectiveWordCount struct{ Text string }

// RouteGuardWordCount is a //nocontract:guard line on a handler that is not one guard name and one
// argument, as it is written.
//
//ergo:code NC-1102
//ergo:message The line {Text} on a handler is not one guard name and one argument.
type RouteGuardWordCount struct{ Text string }

// GuardNameInvalid is a //nocontract:guard line whose guard name is not an exported Go identifier.
//
//ergo:code NC-1103
//ergo:message {Name} in the line {Text} is not an exported Go identifier, the form a guard's name takes.
type GuardNameInvalid struct {
	Text string
	Name string
}

// GuardDirectiveRepeated is a declaration with a second //nocontract:guard line, as it is written.
//
//ergo:code NC-1104
//ergo:message The line {Text} is a second guard line; a declaration names one guard.
type GuardDirectiveRepeated struct{ Text string }

func (RouteDirectiveWordCount) isProblem()  {}
func (RouteMethodUnknown) isProblem()       {}
func (StatusDirectiveWordCount) isProblem() {}
func (StatusNotNumber) isProblem()          {}
func (StatusDirectiveRepeated) isProblem()  {}
func (GuardDirectiveWordCount) isProblem()  {}
func (RouteGuardWordCount) isProblem()      {}
func (GuardNameInvalid) isProblem()         {}
func (GuardDirectiveRepeated) isProblem()   {}
