package nocontract

import "core:fmt"

// Object_Field_Step names one field the checked object declares.
Object_Field_Step :: struct #all_or_none {
	name: string,
}

// Dictionary_Key_Step names the actual key of one dictionary entry.
Dictionary_Key_Step :: struct #all_or_none {
	key: string,
}

// List_Item_Step names the zero-based position of one list value.
List_Item_Step :: struct #all_or_none {
	index: int,
}

// Body_Path_Step keeps field, dictionary and list positions distinct.
Body_Path_Step :: union #no_nil {
	Object_Field_Step,
	Dictionary_Key_Step,
	List_Item_Step,
}

// body_child_path writes the same JSON location for request refusals and answer conversion.
@(require_results)
body_child_path :: proc(parent: string, step: Body_Path_Step) -> string {
	switch child in step {
	case Object_Field_Step: return fmt.tprintf("%s.%s", parent, child.name)
	case Dictionary_Key_Step: return fmt.tprintf("%s[%q]", parent, child.key)
	case List_Item_Step: return fmt.tprintf("%s[%d]", parent, child.index)
	}
	unreachable()
}
