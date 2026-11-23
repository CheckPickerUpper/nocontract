package projectfile

import (
	"maps"
	"path/filepath"
	"slices"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/internal/native"
)

func (parse *projectParse) backendWithCollections(backend Backend, fields map[string]jsonField) Backend {
	field, specified := fields["collections"]
	if !specified {
		return backend
	}
	if _, isOdin := backend.(OdinBackend); !isOdin {
		parse.refuse(field.keyStart, KeyUnknown{Key: "collections"})
		return backend
	}
	object, isObject := field.value.shape.(jsonObject)
	if !isObject {
		parse.refuse(field.value.start, ValueWrongType{Key: "collections", Written: field.value.written, Want: JSONObject{}})
		return backend
	}
	named := parse.fields(object, "collections", nil)
	collections := make([]native.OdinCollection, 0, len(named))
	for _, name := range slices.Sorted(maps.Keys(named)) {
		outcome.ForEachOption(parse.text(keyPath("collections", name), named[name].value), func(directory string) {
			if !filepath.IsAbs(directory) {
				directory = filepath.Join(filepath.Dir(parse.file), directory)
			}
			collections = append(collections, native.OdinCollection{Name: name, Directory: filepath.Clean(directory)})
		})
	}
	return OdinBackend{collections: collections}
}
