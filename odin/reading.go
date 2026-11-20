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
	"github.com/CheckPickerUpper/nocontract/internal/native"
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
	request  native.Request
	snapshot compiler.Snapshot
	structs  []assembling.DeclaredStruct
	members  []assembling.DeclaredMember
}

type routeBinding struct {
	declaration compiler.Declaration
	endpoint    string
	input       int
	output      compiler.Union
	guard       outcome.Option[guardBinding]
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
	return outcome.FoldOption(outcome.EmptyableToOption(os.MkdirAll(filepath.Join(request.Directory, ".scratch"), 0o755)), outcome.Err[native.Project], func() outcome.Result[native.Project] {
		return outcome.Fold(compiler.Read(context, request.Directory, request.Packages), func(snapshot compiler.Snapshot) outcome.Result[native.Project] {
			index := &readIndex{request: request, snapshot: snapshot, structs: []assembling.DeclaredStruct{}, members: []assembling.DeclaredMember{}}
			return index.project()
		}, outcome.Err[native.Project])
	})
}

func (index *readIndex) project() outcome.Result[native.Project] {
	packages := map[string]string{}
	for _, declaration := range index.snapshot.Declarations {
		directory := filepath.Dir(declaration.File)
		if previous, present := packages[declaration.Entity.Package]; present && previous != directory {
			return outcome.Err[native.Project](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Package, Rule: "package name also belongs to " + previous + "; native package names must be unique across project packages"})
		}
		packages[declaration.Entity.Package] = directory
		for _, comment := range declaration.Comments {
			if body, present := strings.CutPrefix(comment, "//nocontract:"); present {
				words := strings.Fields(body)
				if len(words) == 0 {
					return outcome.Err[native.Project](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: "unknown directive " + comment})
				}
				switch words[0] {
				case "route", "guard":
					if declaration.Entity.Kind != "Procedure" {
						return outcome.Err[native.Project](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: comment + " belongs on a procedure"})
					}
				case "status", "instant":
					if declaration.Entity.Kind != "Type" {
						return outcome.Err[native.Project](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: comment + " belongs on a type"})
					}
				default:
					return outcome.Err[native.Project](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: "unknown directive " + comment})
				}
			}
		}
	}
	bindings := outcome.Ok([]routeBinding{})
	for _, root := range index.snapshot.Graph.Roots {
		declaration := index.declaration(root.Package, root.Name)
		bindings = outcome.Fold(bindings, func(previous []routeBinding) outcome.Result[[]routeBinding] {
			return outcome.FoldOption(declaration, func(declaration compiler.Declaration) outcome.Result[[]routeBinding] {
				return outcome.Fold(index.route(declaration, root), func(route outcome.Option[routeBinding]) outcome.Result[[]routeBinding] {
					return outcome.FoldOption(route, func(route routeBinding) outcome.Result[[]routeBinding] { return outcome.Ok(append(previous, route)) }, func() outcome.Result[[]routeBinding] { return outcome.Ok(previous) })
				}, outcome.Err[[]routeBinding])
			}, func() outcome.Result[[]routeBinding] { return outcome.Ok(previous) })
		}, outcome.Err[[]routeBinding])
	}
	return outcome.Fold(bindings, func(bindings []routeBinding) outcome.Result[native.Project] {
		if len(bindings) == 0 {
			return outcome.Err[native.Project](DeclarationRefused{Position: index.request.API.Position, Declaration: index.request.Directory, Rule: "no //nocontract:route procedure was declared"})
		}
		groups := []assembling.DeclaredGroup{}
		guarded := []native.GuardedRoute{}
		endpoints := outcome.Ok([]assembling.DeclaredEndpoint{})
		for _, binding := range bindings {
			endpoints = outcome.Fold(endpoints, func(previous []assembling.DeclaredEndpoint) outcome.Result[[]assembling.DeclaredEndpoint] {
				return outcome.Fold(index.endpoint(binding), func(endpoint assembling.DeclaredEndpoint) outcome.Result[[]assembling.DeclaredEndpoint] {
					return outcome.Ok(append(previous, endpoint))
				}, outcome.Err[[]assembling.DeclaredEndpoint])
			}, outcome.Err[[]assembling.DeclaredEndpoint])
		}
		return outcome.Fold(endpoints, func(endpoints []assembling.DeclaredEndpoint) outcome.Result[native.Project] {
			for position, binding := range bindings {
				groupName := binding.declaration.Entity.Package
				group := slices.IndexFunc(groups, func(group assembling.DeclaredGroup) bool { return group.Name == groupName })
				if group < 0 {
					groups = append(groups, assembling.DeclaredGroup{Name: groupName, Position: placed(binding.declaration), Endpoints: []assembling.DeclaredEndpoint{}})
					group = len(groups) - 1
				}
				groups[group].Endpoints = append(groups[group].Endpoints, endpoints[position])
				outcome.ForEachOption(binding.guard, func(guard guardBinding) {
					guarded = append(guarded, native.GuardedRoute{Group: groupName, Endpoint: binding.endpoint, Scope: guard.scope, Refusals: guard.refusals})
				})
			}
			declared := assembling.Declared{API: index.request.API, Groups: groups, Members: index.members, Structs: index.structs}
			arguments := append([]string{"odin", "run", index.request.Server}, compiler.Flags([]string{"nocontract_server"})...)
			arguments = append(arguments, "-out:"+filepath.Join(index.request.Directory, ".scratch", "nocontract-odin-server"))
			return outcome.Ok(native.Project{Declared: declared, Guards: guarded, Command: native.Command{Directory: index.request.Directory, Arguments: arguments}, Generate: func(assembled assembling.APIAssembled) outcome.Result[[]native.File] {
				return generateBindings(index.request, index.snapshot.Graph, bindings, assembled)
			}})
		}, outcome.Err[native.Project])
	}, outcome.Err[native.Project])
}

func (index *readIndex) declaration(packageName, name string) outcome.Option[compiler.Declaration] {
	for _, declaration := range index.snapshot.Declarations {
		if declaration.Entity.Package == packageName && declaration.Entity.Name == name {
			return outcome.Some(declaration)
		}
	}
	return outcome.None[compiler.Declaration]()
}

func placed(declaration compiler.Declaration) assembling.Position {
	return assembling.Position{File: declaration.File, Line: declaration.Line, Column: declaration.Column}
}

func directive(declaration compiler.Declaration, name string) outcome.Result[outcome.Option[string]] {
	texts := []string{}
	for _, comment := range declaration.Comments {
		if text, present := strings.CutPrefix(comment, "//nocontract:"+name); present {
			if text != "" && !strings.HasPrefix(text, " ") {
				continue
			}
			texts = append(texts, strings.TrimSpace(text))
		}
	}
	switch len(texts) {
	case 0:
		return outcome.Ok(outcome.None[string]())
	case 1:
		return outcome.Ok(outcome.Some(texts[0]))
	}
	return outcome.Err[outcome.Option[string]](DeclarationRefused{Position: placed(declaration), Declaration: declaration.Entity.Name, Rule: "repeated //nocontract:" + name + " directive"})
}

func (index *readIndex) route(declaration compiler.Declaration, root compiler.Root) outcome.Result[outcome.Option[routeBinding]] {
	return outcome.Fold(directive(declaration, "route"), func(route outcome.Option[string]) outcome.Result[outcome.Option[routeBinding]] {
		return outcome.FoldOption(route, func(string) outcome.Result[outcome.Option[routeBinding]] {
			return outcome.Fold(requireType[compiler.Procedure](index.snapshot.Graph.Types[root.Type], DeclarationRefused{Position: placed(declaration), Declaration: root.Name, Rule: "route must be a procedure"}), func(procedure compiler.Procedure) outcome.Result[outcome.Option[routeBinding]] {
				if len(procedure.Parameters) != 1 || len(procedure.Results) != 1 {
					return outcome.Err[outcome.Option[routeBinding]](DeclarationRefused{Position: placed(declaration), Declaration: root.Name, Rule: "a route must take one typed input struct and return one named union"})
				}
				input, output := procedure.Parameters[0], procedure.Results[0]
				return outcome.Fold(requireType[compiler.Named](index.snapshot.Graph.Types[input], DeclarationRefused{Position: placed(declaration), Declaration: root.Name, Rule: "input must be named"}), func(compiler.Named) outcome.Result[outcome.Option[routeBinding]] {
					return outcome.Fold(requireType[compiler.Struct](index.base(input), DeclarationRefused{Position: placed(declaration), Declaration: root.Name, Rule: "input must be a struct"}), func(compiler.Struct) outcome.Result[outcome.Option[routeBinding]] {
						return outcome.Fold(requireType[compiler.Named](index.snapshot.Graph.Types[output], DeclarationRefused{Position: placed(declaration), Declaration: root.Name, Rule: "outcome must be named"}), func(compiler.Named) outcome.Result[outcome.Option[routeBinding]] {
							return outcome.Fold(requireType[compiler.Union](index.base(output), DeclarationRefused{Position: placed(declaration), Declaration: root.Name, Rule: "outcome must be a union"}), func(union compiler.Union) outcome.Result[outcome.Option[routeBinding]] {
								return outcome.Fold(index.readGuard(declaration), func(guard outcome.Option[guardBinding]) outcome.Result[outcome.Option[routeBinding]] {
									return outcome.Ok(outcome.Some(routeBinding{declaration: declaration, endpoint: description.EndpointName(root.Name), input: input, output: union, guard: guard}))
								}, outcome.Err[outcome.Option[routeBinding]])
							}, outcome.Err[outcome.Option[routeBinding]])
						}, outcome.Err[outcome.Option[routeBinding]])
					}, outcome.Err[outcome.Option[routeBinding]])
				}, outcome.Err[outcome.Option[routeBinding]])
			}, outcome.Err[outcome.Option[routeBinding]])
		}, func() outcome.Result[outcome.Option[routeBinding]] { return outcome.Ok(outcome.None[routeBinding]()) })
	}, outcome.Err[outcome.Option[routeBinding]])
}

func (index *readIndex) base(id int) compiler.Type {
	return compiler.Base(index.snapshot.Graph, id)
}

func requireType[T compiler.Type](checked compiler.Type, refused DeclarationRefused) outcome.Result[T] {
	return outcome.FoldOption(compiler.Select[T](checked), outcome.Ok[T], func() outcome.Result[T] { return outcome.Err[T](refused) })
}

func (index *readIndex) endpoint(binding routeBinding) outcome.Result[assembling.DeclaredEndpoint] {
	return outcome.Fold(directive(binding.declaration, "route"), func(text outcome.Option[string]) outcome.Result[assembling.DeclaredEndpoint] {
		return outcome.FoldOption(text, func(text string) outcome.Result[assembling.DeclaredEndpoint] {
			words := strings.Fields(text)
			if len(words) != 2 {
				return outcome.Err[assembling.DeclaredEndpoint](DeclarationRefused{Position: placed(binding.declaration), Declaration: binding.declaration.Entity.Name, Rule: "route directive requires METHOD /path"})
			}
			return outcome.FoldOption(description.ParseMethod(words[0]), func(method description.Method) outcome.Result[assembling.DeclaredEndpoint] {
				switch path := description.ParseRoutePath(words[1]).(type) {
				case description.RoutePathRead:
					return index.boundEndpoint(binding, method, path.Path)
				case description.RoutePathRefused:
					return outcome.Err[assembling.DeclaredEndpoint](DeclarationRefused{Position: placed(binding.declaration), Declaration: binding.declaration.Entity.Name, Rule: path.Problem.Report(indexLanguage()).Message})
				case nil:
				}
				return outcome.Err[assembling.DeclaredEndpoint](DeclarationRefused{Position: placed(binding.declaration), Declaration: binding.declaration.Entity.Name, Rule: "route path has no parsed outcome"})
			}, func() outcome.Result[assembling.DeclaredEndpoint] {
				return outcome.Err[assembling.DeclaredEndpoint](DeclarationRefused{Position: placed(binding.declaration), Declaration: binding.declaration.Entity.Name, Rule: "unsupported HTTP method " + words[0]})
			})
		}, func() outcome.Result[assembling.DeclaredEndpoint] {
			return outcome.Err[assembling.DeclaredEndpoint](DeclarationRefused{Position: placed(binding.declaration), Declaration: binding.declaration.Entity.Name, Rule: "route directive missing"})
		})
	}, outcome.Err[assembling.DeclaredEndpoint])
}

func (index *readIndex) boundEndpoint(binding routeBinding, method description.Method, path description.RoutePath) outcome.Result[assembling.DeclaredEndpoint] {
	return outcome.Fold(requireType[compiler.Struct](index.base(binding.input), DeclarationRefused{Position: placed(binding.declaration), Declaration: binding.declaration.Entity.Name, Rule: "input must be a struct"}), func(input compiler.Struct) outcome.Result[assembling.DeclaredEndpoint] {
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
			endpoint := assembling.DeclaredEndpoint{Name: binding.endpoint, Method: method, Path: path, Params: []description.Field{}, Query: []description.Field{}, Body: description.NoBody{}, Successes: description.Successes{First: successes[0], Rest: successes[1:]}, Failures: failures, Position: placed(binding.declaration)}
			read := outcome.Ok(endpoint)
			for _, field := range input.Fields {
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
	return outcome.Fold(requireType[compiler.Named](index.snapshot.Graph.Types[id], DeclarationRefused{Position: index.request.API.Position, Declaration: "outcome member", Rule: "an outcome member must be a named struct"}), func(checked compiler.Named) outcome.Result[description.Member] {
		return outcome.FoldOption(index.declaration(checked.Package, checked.Name), func(declaration compiler.Declaration) outcome.Result[description.Member] {
			for _, existing := range index.members {
				if existing.Member.Name == checked.Name && existing.Position == placed(declaration) {
					return outcome.Ok(existing.Member)
				}
			}
			return outcome.Fold(requireType[compiler.Struct](index.base(id), DeclarationRefused{Position: placed(declaration), Declaration: checked.Name, Rule: "an outcome member must be a named struct"}), func(structure compiler.Struct) outcome.Result[description.Member] {
				return outcome.Fold(index.fields(structure.Fields, []int{id}, placed(declaration)), func(fields []description.Field) outcome.Result[description.Member] {
					return outcome.Fold(memberAnswer(declaration), func(answer description.Answer) outcome.Result[description.Member] {
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

func memberAnswer(declaration compiler.Declaration) outcome.Result[description.Answer] {
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
	return outcome.Fold(directive(declaration, "status"), func(text outcome.Option[string]) outcome.Result[description.Answer] {
		return outcome.FoldOption(text, func(text string) outcome.Result[description.Answer] {
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
								return outcome.Fold(parseFailureCode(code, declaration), func(code ergo.Code) outcome.Result[description.Answer] {
									return outcome.Ok[description.Answer](description.Fails{Status: status, Code: code})
								}, outcome.Err[description.Answer])
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
	}, outcome.Err[description.Answer])
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
