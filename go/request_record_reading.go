package nocontract

// BodyField is a required member of a record and the function that reads its value.
type BodyField[V any] struct {
	Name string
	Read func(BodyValue) BodyReading[V]
}

// BodyRecord reads a record whose members each occur exactly once.
func BodyRecord[V any](fields []BodyField[V]) func(BodyValue) BodyReading[map[string]V] {
	readers := make(map[string]func(BodyValue) BodyReading[V], len(fields))
	for _, field := range fields {
		readers[field.Name] = field.Read
	}
	return func(value BodyValue) BodyReading[map[string]V] {
		read := BodyObject(value, make(map[string]V, len(fields)), func(object map[string]V, member BodyMember) BodyReading[map[string]V] {
			readField, declared := readers[member.Name]
			if !declared {
				return Unknown[map[string]V](member)
			}
			if _, alreadyRead := object[member.Name]; alreadyRead {
				return BodyRefused{Cause: BodyMemberRepeated{Path: member.Value.path}}
			}
			return Next(readField(member.Value), func(held V) BodyReading[map[string]V] {
				object[member.Name] = held
				return Read[map[string]V]{Value: object}
			})
		})
		return Next(read, func(object map[string]V) BodyReading[map[string]V] {
			for _, field := range fields {
				if _, present := object[field.Name]; !present {
					return BodyRefused{Cause: BodyMemberMissing{Path: value.member(field.Name).path}}
				}
			}
			return Read[map[string]V]{Value: object}
		})
	}
}
