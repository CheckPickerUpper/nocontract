// Package odin reads compiler-checked Odin routes and generates typed native bindings.
package odin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	ergo "github.com/CheckPickerUpper/ErGo"
	"github.com/CheckPickerUpper/outcome"
	"github.com/fatih/structtag"

	"github.com/CheckPickerUpper/nocontract/assembling"
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
	"github.com/CheckPickerUpper/nocontract/internal/native"
	"github.com/CheckPickerUpper/nocontract/internal/sequence"
	"github.com/CheckPickerUpper/nocontract/odin/compiler"
)

// DeclarationRefused retains the native declaration, source location, and rule it violates.
type DeclarationRefused struct {
	Position    assembling.Position
	Declaration string
	Rule        string
}

// Error describes the declaration the compiler-backed reader cannot expose.
func (failure DeclarationRefused) Error() string {
	return fmt.Sprintf("%s Odin declaration %s: %s", failure.Position.String(), failure.Declaration, failure.Rule)
}

type readIndex struct {
	request       native.Request
	declarations  []compiler.Declaration
	documentation documentedProject
	graph         compiler.Graph
	structs       []assembling.DeclaredStruct
	members       []assembling.DeclaredMember
}

type routeBinding struct {
	declaration compiler.Declaration
	endpoint    string
	method      description.Method
	path        description.RoutePath
	input       int
	output      compiler.Union
	guard       outcome.Option[guardBinding]
	headers     []headerDirective
}

type guardBinding struct {
	declaration compiler.Declaration
	output      compiler.Union
	scope       string
	refusals    []description.Member
}

// Read compiles the project's routes and returns their common API description and
// the generator for an Odin program invoking their actual procedures.
func Read(context context.Context, request native.Request) outcome.Result[native.Project] {
	directory := filepath.Join(request.Directory, ".scratch")
	return outcome.FoldOption(outcome.EmptyableToOption(os.MkdirAll(directory, 0o755)), outcome.Err[native.Project], func() outcome.Result[native.Project] {
		return outcome.Fold(outcome.TupleToResult(os.MkdirTemp(directory, "nocontract-odin-")), func(scratch string) outcome.Result[native.Project] {
			defer func() { _ = os.RemoveAll(scratch) }()
			return readProject(context, request, scratch)
		}, outcome.Err[native.Project])
	})
}

func readProject(context context.Context, request native.Request, scratch string) outcome.Result[native.Project] {
	return outcome.Fold(compiler.ReadDeclarations(context, request.Directory, scratch, request.Packages, request.OdinCollections...), func(declarations []compiler.Declaration) outcome.Result[native.Project] {
		return outcome.Fold(readDocumentation(declarations), func(documentation documentedProject) outcome.Result[native.Project] {
			if len(documentation.routes) == 0 {
				return outcome.Err[native.Project](DeclarationRefused{Position: request.API.Position, Declaration: request.Directory, Rule: "no //nocontract:route procedure was declared"})
			}
			procedures := reflectedProcedures(declarations, documentation.routes)
			return outcome.Fold(compiler.ReflectProcedures(context, request.Directory, scratch, request.Packages, procedures, request.OdinCollections...), func(graph compiler.Graph) outcome.Result[native.Project] {
				index := &readIndex{request: request, declarations: declarations, documentation: documentation, graph: graph, structs: []assembling.DeclaredStruct{}, members: []assembling.DeclaredMember{}}
				return index.project()
			}, outcome.Err[native.Project])
		}, outcome.Err[native.Project])
	}, outcome.Err[native.Project])
}

func (index *readIndex) project() outcome.Result[native.Project] {
	return outcome.Fold(sequence.Traverse(index.documentation.routes, index.route), func(bindings []routeBinding) outcome.Result[native.Project] {
		groups := []assembling.DeclaredGroup{}
		return outcome.Fold(sequence.Traverse(bindings, index.endpoint), func(endpoints []assembling.DeclaredEndpoint) outcome.Result[native.Project] {
			for position, binding := range bindings {
				groupName := binding.declaration.Entity.Package
				group := slices.IndexFunc(groups, func(group assembling.DeclaredGroup) bool { return group.Name == groupName })
				if group < 0 {
					groups = append(groups, assembling.DeclaredGroup{Name: groupName, Position: placed(binding.declaration), Endpoints: []assembling.DeclaredEndpoint{}})
					group = len(groups) - 1
				}
				groups[group].Endpoints = append(groups[group].Endpoints, endpoints[position])
			}
			declared := assembling.Declared{API: index.request.API, Groups: groups, Members: index.members, Structs: index.structs}
			return outcome.Ok(native.Project{Declared: declared, Generate: func(checked assembling.APIAssembled) outcome.Result[[]native.File] {
				return outcome.Fold(generateBindings(index.request, index.graph, bindings, checked), func(files []native.File) outcome.Result[[]native.File] {
					return outcome.Fold(generateFilesTest(index.request), func(test native.File) outcome.Result[[]native.File] {
						return outcome.Ok(append(files, test))
					}, outcome.Err[[]native.File])
				}, outcome.Err[[]native.File])
			}})
		}, outcome.Err[native.Project])
	}, outcome.Err[native.Project])
}

func (index *readIndex) declaration(packageName, name string) outcome.Option[compiler.Declaration] {
	for _, declaration := range index.declarations {
		if declaration.Entity.Package == packageName && declaration.Entity.Name == name {
			return outcome.Some(declaration)
		}
	}
	return outcome.None[compiler.Declaration]()
}

func placed(declaration compiler.Declaration) assembling.Position {
	return assembling.Position{File: declaration.File, Line: declaration.Line, Column: declaration.Column}
}

func (index *readIndex) route(source routeSource) outcome.Result[routeBinding] {
	declaration := source.declaration
	refused := func(rule string) DeclarationRefused {
		return DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: rule}
	}
	root := slices.IndexFunc(index.graph.Roots, func(root compiler.Root) bool {
		return root.Package == declaration.Entity.Package && root.Name == declaration.Entity.Name
	})
	if root < 0 {
		return outcome.Err[routeBinding](refused("compiled reflection did not include this route procedure"))
	}
	return outcome.Fold(requireType[compiler.Procedure](index.graph.Types[index.graph.Roots[root].Type], refused("route must be a procedure")), func(procedure compiler.Procedure) outcome.Result[routeBinding] {
		if len(procedure.Parameters) != 1 || len(procedure.Results) != 1 {
			return outcome.Err[routeBinding](refused("a route must take one typed input struct and return one named union"))
		}
		input, output := procedure.Parameters[0], procedure.Results[0]
		return outcome.Fold(requireType[compiler.Named](index.graph.Types[input], refused("input must be named")), func(compiler.Named) outcome.Result[routeBinding] {
			return outcome.Fold(requireType[compiler.Named](index.graph.Types[output], refused("outcome must be named")), func(compiler.Named) outcome.Result[routeBinding] {
				return outcome.Fold(requireType[compiler.Union](index.base(output), refused("outcome must be a union")), func(union compiler.Union) outcome.Result[routeBinding] {
					return outcome.Fold(index.readGuard(source), func(guard outcome.Option[guardBinding]) outcome.Result[routeBinding] {
						return outcome.Ok(routeBinding{declaration: declaration, endpoint: description.EndpointName(declaration.Entity.Name), method: source.method, path: source.path, input: input, output: union, guard: guard, headers: source.headers})
					}, outcome.Err[routeBinding])
				}, outcome.Err[routeBinding])
			}, outcome.Err[routeBinding])
		}, outcome.Err[routeBinding])
	}, outcome.Err[routeBinding])
}

func (index *readIndex) base(id int) compiler.Type {
	return compiler.Base(index.graph, id)
}

func requireType[T compiler.Type](checked compiler.Type, refused DeclarationRefused) outcome.Result[T] {
	return outcome.FoldOption(compiler.Select[T](checked), outcome.Ok[T], func() outcome.Result[T] { return outcome.Err[T](refused) })
}

func (index *readIndex) endpoint(binding routeBinding) outcome.Result[assembling.DeclaredEndpoint] {
	path := binding.path
	return outcome.Fold(requireType[compiler.Struct](index.base(binding.input), DeclarationRefused{Position: placed(binding.declaration), Declaration: binding.declaration.Entity.Name, Rule: "input must be a struct"}), func(input compiler.Struct) outcome.Result[assembling.DeclaredEndpoint] {
		return outcome.Fold(index.readHeaders(binding, input), func(headers []assembling.DeclaredHeader) outcome.Result[assembling.DeclaredEndpoint] {
			return outcome.Fold(index.outcomeMembers(binding.output), func(members []description.Member) outcome.Result[assembling.DeclaredEndpoint] {
				successes, failures := []description.Member{}, []description.Member{}
				for _, member := range members {
					switch member.Answer.(type) {
					case description.Succeeds:
						successes = append(successes, member)
					case description.Fails:
						failures = append(failures, member)
					case nil:
					}
				}
				if len(successes) == 0 {
					return outcome.Err[assembling.DeclaredEndpoint](DeclarationRefused{Position: placed(binding.declaration), Declaration: binding.declaration.Entity.Name, Rule: "route outcome has no success"})
				}
				outcome.ForEachOption(binding.guard, func(guard guardBinding) { failures = append(failures, guard.refusals...) })
				endpoint := assembling.DeclaredEndpoint{Name: binding.endpoint, Method: binding.method, Path: path, Params: []description.Field{}, Query: []description.Field{}, Headers: headers, Body: description.NoBody{}, Successes: description.Successes{First: successes[0], Rest: successes[1:]}, Failures: failures, Position: placed(binding.declaration)}
				headerFields := map[string]struct{}{}
				for _, header := range headers {
					headerFields[header.Field.Name.Text()] = struct{}{}
				}
				read := outcome.Ok(endpoint)
				for _, field := range input.Fields {
					if _, header := headerFields[field.Name]; header {
						continue
					}
					read = outcome.Fold(read, func(endpoint assembling.DeclaredEndpoint) outcome.Result[assembling.DeclaredEndpoint] {
						return outcome.Fold(index.schema(field.Type, []int{}, placed(binding.declaration)), func(schema description.Schema) outcome.Result[assembling.DeclaredEndpoint] {
							if field.Name == "body" {
								endpoint.Body = description.JSONBody{Schema: schema}
								return outcome.Ok(endpoint)
							}
							return outcome.Fold(namedField(field.Name, schema, placed(binding.declaration)), func(described description.Field) outcome.Result[assembling.DeclaredEndpoint] {
								bound := false
								for _, segment := range path.Segments() {
									switch segment := segment.(type) {
									case description.RouteParameter:
										if segment.Name() == field.Name {
											bound = true
										}
									case description.RouteText, nil:
									}
								}
								if bound {
									endpoint.Params = append(endpoint.Params, described)
								} else {
									endpoint.Query = append(endpoint.Query, described)
								}
								return outcome.Ok(endpoint)
							}, outcome.Err[assembling.DeclaredEndpoint])
						}, outcome.Err[assembling.DeclaredEndpoint])
					}, outcome.Err[assembling.DeclaredEndpoint])
				}
				return read
			}, outcome.Err[assembling.DeclaredEndpoint])
		}, outcome.Err[assembling.DeclaredEndpoint])
	}, outcome.Err[assembling.DeclaredEndpoint])
}

// HeaderDeclarationRefused keeps a header binding, its source and its precise declaration cause.
type HeaderDeclarationRefused struct {
	Position assembling.Position
	Field    string
	Name     string
	Problem  assembling.Problem
}

// Error reports the shared permanent code and the declaration's original location.
func (failure HeaderDeclarationRefused) Error() string {
	return fmt.Sprintf("%s: %s: %s", failure.Position, failure.Problem.ErrorCode(), failure.Problem.Report(indexLanguage()).Message)
}

func (index *readIndex) readHeaders(binding routeBinding, input compiler.Struct) outcome.Result[[]assembling.DeclaredHeader] {
	fields, available := map[string]compiler.Field{}, []string{}
	for _, field := range input.Fields {
		fields[field.Name] = field
		available = append(available, field.Name)
	}
	position := placed(binding.declaration)
	return sequence.Traverse(binding.headers, func(header headerDirective) outcome.Result[assembling.DeclaredHeader] {
		refuse := func(problem assembling.Problem) outcome.Result[assembling.DeclaredHeader] {
			return outcome.Err[assembling.DeclaredHeader](HeaderDeclarationRefused{Position: position, Field: header.field, Name: header.name, Problem: problem})
		}
		return outcome.FoldOption(keyed.Entry(fields, header.field), func(field compiler.Field) outcome.Result[assembling.DeclaredHeader] {
			if field.Name == "body" {
				return refuse(assembling.HeaderBindingConflict{Name: header.name, Field: header.field, Part: "body", Binding: "body"})
			}
			for _, segment := range binding.path.Segments() {
				switch segment := segment.(type) {
				case description.RouteParameter:
					if segment.Name() == field.Name {
						return refuse(assembling.HeaderBindingConflict{Name: header.name, Field: header.field, Part: "path", Binding: segment.Name()})
					}
				case description.RouteText, nil:
				}
			}
			return outcome.Fold(index.schema(field.Type, []int{}, position), func(schema description.Schema) outcome.Result[assembling.DeclaredHeader] {
				return outcome.Fold(namedField(field.Name, schema, position), func(described description.Field) outcome.Result[assembling.DeclaredHeader] {
					return outcome.Ok(assembling.DeclaredHeader{Name: header.name, Field: described, Position: position})
				}, outcome.Err[assembling.DeclaredHeader])
			}, outcome.Err[assembling.DeclaredHeader])
		}, func() outcome.Result[assembling.DeclaredHeader] {
			return refuse(assembling.HeaderFieldUndeclared{Name: header.name, Field: header.field, Available: strings.Join(available, ", ")})
		})
	})
}

func namedField(name string, schema description.Schema, position assembling.Position) outcome.Result[description.Field] {
	switch parsed := description.ParseFieldName(name).(type) {
	case description.FieldNameRead:
		return outcome.Ok(description.Field{Name: parsed.Name, Schema: schema})
	case description.FieldNameRefused:
		return outcome.Err[description.Field](DeclarationRefused{Position: position, Declaration: name, Rule: parsed.Problem.Report(indexLanguage()).Message})
	case nil:
	}
	return outcome.Err[description.Field](DeclarationRefused{Position: position, Declaration: name, Rule: "field name has no parsed outcome"})
}

func (index *readIndex) outcomeMembers(union compiler.Union) outcome.Result[[]description.Member] {
	members := outcome.Ok([]description.Member{})
	for _, variant := range union.Variants {
		members = outcome.Fold(members, func(previous []description.Member) outcome.Result[[]description.Member] {
			return outcome.Fold(index.member(variant), func(member description.Member) outcome.Result[[]description.Member] {
				return outcome.Ok(append(previous, member))
			}, outcome.Err[[]description.Member])
		}, outcome.Err[[]description.Member])
	}
	return members
}

func (index *readIndex) member(id int) outcome.Result[description.Member] {
	return outcome.Fold(requireType[compiler.Named](index.graph.Types[id], DeclarationRefused{Position: index.request.API.Position, Declaration: "outcome member", Rule: "an outcome member must be a named struct"}), func(checked compiler.Named) outcome.Result[description.Member] {
		return outcome.FoldOption(index.declaration(checked.Package, checked.Name), func(declaration compiler.Declaration) outcome.Result[description.Member] {
			for _, existing := range index.members {
				if existing.Member.Name == checked.Name && existing.Position == placed(declaration) {
					return outcome.Ok(existing.Member)
				}
			}
			return outcome.Fold(requireType[compiler.Struct](index.base(id), DeclarationRefused{Position: placed(declaration), Declaration: checked.Name, Rule: "an outcome member must be a named struct"}), func(structure compiler.Struct) outcome.Result[description.Member] {
				return outcome.Fold(index.fields(structure.Fields, []int{id}, placed(declaration)), func(fields []description.Field) outcome.Result[description.Member] {
					return outcome.Fold(index.memberAnswer(declaration), func(answer description.Answer) outcome.Result[description.Member] {
						member := description.Member{Name: checked.Name, Answer: answer, Fields: fields}
						index.members = append(index.members, assembling.DeclaredMember{Member: member, Position: placed(declaration)})
						return outcome.Ok(member)
					}, outcome.Err[description.Member])
				}, outcome.Err[description.Member])
			}, outcome.Err[description.Member])
		}, func() outcome.Result[description.Member] {
			return outcome.Err[description.Member](DeclarationRefused{Position: assembling.Position{File: checked.File, Line: checked.Line, Column: checked.Column}, Declaration: checked.Name, Rule: "outcome member is declared outside the project packages"})
		})
	}, outcome.Err[description.Member])
}

func (index *readIndex) memberAnswer(declaration compiler.Declaration) outcome.Result[description.Answer] {
	code, success := "", false
	for _, attribute := range declaration.Entity.Attributes {
		switch attribute.Name {
		case "error_code":
			code = attribute.Value
		case "not_an_error":
			success = true
		}
	}
	if (code == "") == !success {
		return outcome.Err[description.Answer](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: "member requires exactly one @(error_code = \"...\") or @(not_an_error)"})
	}
	return outcome.FoldOption(index.documentation.typeDirective(declaration, "status"), func(text string) outcome.Result[description.Answer] {
		return outcome.Fold(outcome.TupleToResult(strconv.Atoi(text)), func(number int) outcome.Result[description.Answer] {
			return description.FoldStatus(number, func(status description.Status) outcome.Result[description.Answer] {
				switch status := status.(type) {
				case description.SuccessStatus:
					if success {
						return outcome.Ok[description.Answer](description.Succeeds{Status: status})
					}
				case description.FailureStatus:
					if !success {
						return outcome.Fold(outcome.TupleToResult(strconv.Unquote(code)), func(code string) outcome.Result[description.Answer] {
							return outcome.Fold(native.ParseFailureCode(code), func(code ergo.Code) outcome.Result[description.Answer] {
								return outcome.Ok[description.Answer](description.Fails{Status: status, Code: code})
							}, func(cause error) outcome.Result[description.Answer] {
								return outcome.Err[description.Answer](DeclarationCause{Position: placed(declaration), Declaration: declaration.Entity.Name, Cause: cause})
							})
						}, func(cause error) outcome.Result[description.Answer] {
							return outcome.Err[description.Answer](DeclarationCause{Position: placed(declaration), Declaration: declaration.Entity.Name, Cause: cause})
						})
					}
				case nil:
				}
				return outcome.Err[description.Answer](MemberStatusContradiction{Position: placed(declaration), Member: declaration.Entity.Name, Status: status, Attributes: slices.Clone(declaration.Entity.Attributes)})
			}, func(cause description.StatusUnanswerable) outcome.Result[description.Answer] {
				return outcome.Err[description.Answer](DeclarationCause{Position: placed(declaration), Declaration: declaration.Entity.Name, Cause: cause})
			})
		}, func(cause error) outcome.Result[description.Answer] {
			return outcome.Err[description.Answer](DeclarationCause{Position: placed(declaration), Declaration: declaration.Entity.Name, Cause: cause})
		})
	}, func() outcome.Result[description.Answer] {
		return outcome.Err[description.Answer](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: "member requires //nocontract:status N"})
	})
}

func wireName(field compiler.Field, position assembling.Position) outcome.Result[string] {
	if field.Using {
		return outcome.Err[string](DeclarationRefused{Position: position, Declaration: field.Name, Rule: "using fields have no single JSON field"})
	}
	return outcome.Fold(outcome.TupleToResult(structtag.Parse(field.Tag)), func(tags *structtag.Tags) outcome.Result[string] {
		return outcome.Fold(outcome.TupleToResult(tags.Get("json")), func(tag *structtag.Tag) outcome.Result[string] {
			if len(tag.Options) > 0 || tag.Name == "-" {
				return outcome.Err[string](DeclarationRefused{Position: position, Declaration: field.Name, Rule: "JSON fields must be present and cannot use tag options"})
			}
			if tag.Name == "" {
				return outcome.Ok(field.Name)
			}
			return outcome.Ok(tag.Name)
		}, func(error) outcome.Result[string] { return outcome.Ok(field.Name) })
	}, outcome.Err[string])
}
