// Package projectfile reads a project file, nocontract.json, into the project it describes: every
// value parsed into its own type, or every place the file is wrong.
package projectfile

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CheckPickerUpper/outcome"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

// Project is a project file read: the file itself, its backend's language, the package patterns
// holding its routes, the TypeScript file to write, the API's name and where it is written, the
// OpenAPI file to write and the folder of the server package to write. Every path is absolute.
// Only Read makes one.
type Project struct {
	file       string
	backend    Backend
	packages   []string
	typeScript outcome.Option[string]
	api        assembling.NamedAPI
	openAPI    OpenAPIFile
	server     string
}

// File is the project file's absolute path.
func (project Project) File() string { return project.file }

// Directory is the folder holding the project file, which package patterns are read from.
func (project Project) Directory() string { return filepath.Dir(project.file) }

// Backend is the language the backend is written in.
func (project Project) Backend() Backend { return project.backend }

// Packages is every package pattern holding routes, none of them empty, at least one.
func (project Project) Packages() []string { return slices.Clone(project.packages) }

// TypeScriptFile is the configured TypeScript output, if the project has one.
func (project Project) TypeScriptFile() outcome.Option[string] { return project.typeScript }

// API is the API's name and where the project file gives it.
func (project Project) API() assembling.NamedAPI { return project.api }

// OpenAPI is the OpenAPI document to write.
func (project Project) OpenAPI() OpenAPIFile { return project.openAPI }

// ServerFolder is the folder of the server package to write.
func (project Project) ServerFolder() string { return project.server }

// OpenAPIFile is the OpenAPI document a project writes: its absolute path and the version of the
// API it declares, neither empty.
type OpenAPIFile struct {
	file    string
	version string
}

// File is the document's absolute path.
func (document OpenAPIFile) File() string { return document.file }

// Version is the version of the API the document declares.
func (document OpenAPIFile) Version() string { return document.version }

// Backend is the language a project's backend is written in.
//
//sumtype:decl
type Backend interface {
	isBackend()
	language() string
}

// GoBackend is a backend written in Go.
//
//ergo:not-an-error
type GoBackend struct{}

// OdinBackend is a backend written in Odin.
//
//ergo:not-an-error
type OdinBackend struct{}

// GleamBackend is a backend written in Gleam.
//
//ergo:not-an-error
type GleamBackend struct{}

func (GoBackend) isBackend()    {}
func (OdinBackend) isBackend()  {}
func (GleamBackend) isBackend() {}

func (GoBackend) language() string    { return "go" }
func (OdinBackend) language() string  { return "odin" }
func (GleamBackend) language() string { return "gleam" }

func backends() []Backend { return []Backend{GoBackend{}, OdinBackend{}, GleamBackend{}} }

// Read is read applied to the project projectFile describes, unreadable applied to the error the
// operating system gave reading it, or refused applied to every place it is wrong, in file order.
func Read[R any](projectFile string, read func(Project) R, unreadable func(error) R, refused func([]assembling.Refusal) R) R {
	return outcome.Fold(outcome.TupleToResult(os.ReadFile(projectFile)), func(content []byte) R {
		parse := &projectParse{file: projectFile, content: content, refusals: []assembling.Refusal{}}
		return foldJSON(content, func(root readValue) R {
			return outcome.FoldOption(parse.project(root), func(project Project) R {
				if len(parse.refusals) > 0 {
					return refused(parse.sortedRefusals())
				}
				return read(project)
			}, func() R { return refused(parse.sortedRefusals()) })
		}, func(failure jsonSyntaxFailure) R {
			parse.refuse(failure.offset, ProjectNotJSON{Reason: failure.reason})
			return refused(parse.sortedRefusals())
		})
	}, unreadable)
}

// projectParse is one project file being parsed: its absolute path, its content, and every place
// found wrong so far.
type projectParse struct {
	file     string
	content  []byte
	refusals []assembling.Refusal
}

// at is the place offset is at in the project file.
func (parse *projectParse) at(offset int) assembling.Position {
	before := parse.content[:offset]
	return assembling.Position{File: parse.file, Line: bytes.Count(before, []byte("\n")) + 1, Column: offset - bytes.LastIndexByte(before, '\n')}
}

// refuse records problem at offset.
func (parse *projectParse) refuse(offset int, problem assembling.Problem) {
	parse.refusals = append(parse.refusals, assembling.Refusal{Position: parse.at(offset), Problem: problem})
}

// sortedRefusals is every refusal, in the order of the places they are at.
func (parse *projectParse) sortedRefusals() []assembling.Refusal {
	return slices.SortedStableFunc(slices.Values(parse.refusals), func(first, second assembling.Refusal) int {
		return cmp.Or(cmp.Compare(first.Position.Line, second.Position.Line), cmp.Compare(first.Position.Column, second.Position.Column))
	})
}

// project is the root value read as a project, or none when any of its values is refused.
func (parse *projectParse) project(root readValue) outcome.Option[Project] {
	object, isObject := root.shape.(jsonObject)
	if !isObject {
		parse.refuse(root.start, ProjectNotObject{Written: root.written})
		return outcome.None[Project]()
	}
	fields := parse.fields(object, "", []string{"language", "packages", "typescript", "api", "openapi", "server"})
	directory := filepath.Dir(parse.file)
	language := parse.backend(fields, root.start)
	packages := parse.patterns(fields, root.start)
	typeScript := outcome.FoldOption(keyed.Entry(fields, "typescript"), func(field jsonField) outcome.Option[string] {
		return foldSome(parse.text("typescript", field.value), func(file string) outcome.Option[string] {
			return outcome.Some(filepath.Join(directory, file))
		})
	}, outcome.None[string])
	api := parse.apiName(fields, root.start)
	openAPI := parse.openAPI(fields, root.start)
	server := parse.path(fields, "", "server", root.start)
	return foldSome(language, func(backend Backend) outcome.Option[Project] {
		return foldSome(packages, func(patterns []string) outcome.Option[Project] {
			return foldSome(api, func(named assembling.NamedAPI) outcome.Option[Project] {
				return foldSome(openAPI, func(document OpenAPIFile) outcome.Option[Project] {
					return foldSome(server, func(serverFolder string) outcome.Option[Project] {
						return outcome.Some(Project{
							file: parse.file, backend: backend, packages: patterns, typeScript: typeScript,
							api: named, openAPI: document, server: filepath.Join(directory, serverFolder),
						})
					})
				})
			})
		})
	})
}

// foldSome is next applied to option's value, or none.
func foldSome[T, R any](option outcome.Option[T], next func(T) outcome.Option[R]) outcome.Option[R] {
	return outcome.FoldOption(option, next, outcome.None[R])
}

// keyPath is the key named within the object at prefix, as a refusal names it.
func keyPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// fields is each key of object that names one of keys, with its first field, refusing every other
// key and every key written again.
func (parse *projectParse) fields(object jsonObject, prefix string, keys []string) map[string]jsonField {
	found := map[string]jsonField{}
	for _, field := range object.fields {
		first, isRepeated := found[field.key]
		switch {
		case !slices.Contains(keys, field.key):
			parse.refuse(field.keyStart, KeyUnknown{Key: keyPath(prefix, field.key)})
		case isRepeated:
			parse.refuse(field.keyStart, KeyRepeated{Key: keyPath(prefix, field.key), First: parse.at(first.keyStart)})
		default:
			found[field.key] = field
		}
	}
	return found
}

// required is the value of the key name among fields, refusing it missing at the object that
// starts at objectStart.
func (parse *projectParse) required(fields map[string]jsonField, prefix, name string, objectStart int) outcome.Option[readValue] {
	field, isFound := fields[name]
	if !isFound {
		parse.refuse(objectStart, KeyMissing{Key: keyPath(prefix, name)})
		return outcome.None[readValue]()
	}
	return outcome.Some(field.value)
}

// text is value read as the non-empty string key holds.
func (parse *projectParse) text(key string, value readValue) outcome.Option[string] {
	switch shape := value.shape.(type) {
	case jsonText:
		if shape.text == "" {
			parse.refuse(value.start, ValueEmpty{Key: key, Written: value.written})
			return outcome.None[string]()
		}
		return outcome.Some(shape.text)
	case jsonObject, jsonArray, jsonScalar, nil:
	}
	parse.refuse(value.start, ValueWrongType{Key: key, Written: value.written, Want: JSONText{}})
	return outcome.None[string]()
}

// path is the non-empty string the key name holds among fields.
func (parse *projectParse) path(fields map[string]jsonField, prefix, name string, objectStart int) outcome.Option[string] {
	return foldSome(parse.required(fields, prefix, name, objectStart), func(value readValue) outcome.Option[string] {
		return parse.text(keyPath(prefix, name), value)
	})
}

// backend is the language "language" names.
func (parse *projectParse) backend(fields map[string]jsonField, objectStart int) outcome.Option[Backend] {
	return foldSome(parse.required(fields, "", "language", objectStart), func(value readValue) outcome.Option[Backend] {
		return foldSome(parse.text("language", value), func(language string) outcome.Option[Backend] {
			types := backends()
			known := make([]string, 0, len(types))
			for _, backend := range types {
				name := backend.language()
				if name == language {
					return outcome.Some(backend)
				}
				known = append(known, name)
			}
			parse.refuse(value.start, LanguageUnknown{Language: language, Supported: strings.Join(known, ", ")})
			return outcome.None[Backend]()
		})
	})
}

// apiName is the name "api" gives the API's class, and where it is written.
func (parse *projectParse) apiName(fields map[string]jsonField, objectStart int) outcome.Option[assembling.NamedAPI] {
	return foldSome(parse.required(fields, "", "api", objectStart), func(value readValue) outcome.Option[assembling.NamedAPI] {
		return foldSome(parse.text("api", value), func(text string) outcome.Option[assembling.NamedAPI] {
			switch read := description.ParseAPIName(text).(type) {
			case description.APINameRead:
				return outcome.Some(assembling.NamedAPI{Name: read.Name, Position: parse.at(value.start)})
			case description.APINameNotIdentifier:
				parse.refuse(value.start, read)
			case nil:
			}
			return outcome.None[assembling.NamedAPI]()
		})
	})
}

// patterns is every package pattern "packages" lists, at least one, none empty.
func (parse *projectParse) patterns(fields map[string]jsonField, objectStart int) outcome.Option[[]string] {
	return foldSome(parse.required(fields, "", "packages", objectStart), func(value readValue) outcome.Option[[]string] {
		switch shape := value.shape.(type) {
		case jsonArray:
			if len(shape.items) == 0 {
				parse.refuse(value.start, ValueEmpty{Key: "packages", Written: value.written})
				return outcome.None[[]string]()
			}
			patterns := []outcome.Option[string]{}
			for index, item := range shape.items {
				patterns = append(patterns, parse.text(fmt.Sprintf("packages[%d]", index), item))
			}
			return everySome(patterns, []string{})
		case jsonObject, jsonText, jsonScalar, nil:
		}
		parse.refuse(value.start, ValueWrongType{Key: "packages", Written: value.written, Want: JSONArray{}})
		return outcome.None[[]string]()
	})
}

// everySome is the values of options after read, or none when any is none.
func everySome[T any](options []outcome.Option[T], read []T) outcome.Option[[]T] {
	if len(options) == 0 {
		return outcome.Some(read)
	}
	return foldSome(options[0], func(value T) outcome.Option[[]T] {
		return everySome(options[1:], append(slices.Clone(read), value))
	})
}

// openAPI is the OpenAPI document "openapi" names: its file and the API's version.
func (parse *projectParse) openAPI(fields map[string]jsonField, objectStart int) outcome.Option[OpenAPIFile] {
	return foldSome(parse.required(fields, "", "openapi", objectStart), func(value readValue) outcome.Option[OpenAPIFile] {
		switch shape := value.shape.(type) {
		case jsonObject:
			document := parse.fields(shape, "openapi", []string{"file", "version"})
			file := parse.path(document, "openapi", "file", value.start)
			version := parse.path(document, "openapi", "version", value.start)
			return foldSome(file, func(path string) outcome.Option[OpenAPIFile] {
				return foldSome(version, func(declared string) outcome.Option[OpenAPIFile] {
					return outcome.Some(OpenAPIFile{file: filepath.Join(filepath.Dir(parse.file), path), version: declared})
				})
			})
		case jsonArray, jsonText, jsonScalar, nil:
		}
		parse.refuse(value.start, ValueWrongType{Key: "openapi", Written: value.written, Want: JSONObject{}})
		return outcome.None[OpenAPIFile]()
	})
}
