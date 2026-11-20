package compiler

// Equal compares exact resolved type identities and every resolved argument.
func Equal(left, right Type) bool {
	switch resolved := left.(type) {
	case Named:
		if other, named := right.(Named); named {
			return resolved.Identity == other.Identity && equalTypes(resolved.Arguments, other.Arguments)
		}
	case Tuple:
		if other, tuple := right.(Tuple); tuple {
			return equalTypes(resolved.Elements, other.Elements)
		}
	case FunctionType:
		if other, function := right.(FunctionType); function {
			return equalTypes(resolved.Parameters, other.Parameters) && Equal(resolved.Returning, other.Returning)
		}
	case Variable:
		if other, variable := right.(Variable); variable {
			return resolved.ID == other.ID
		}
	case nil:
	}
	return false
}

func equalTypes(left, right []Type) bool {
	if len(left) != len(right) {
		return false
	}
	for i, each := range left {
		if !Equal(each, right[i]) {
			return false
		}
	}
	return true
}
