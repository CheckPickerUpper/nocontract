package running

import (
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/effect"
	"github.com/CheckPickerUpper/nocontract/internal/projectfile"
	"github.com/CheckPickerUpper/nocontract/openapi"
	"github.com/CheckPickerUpper/outcome"
)

type renderOutput func() outcome.Result[[]byte]

func renderedOutput(content []byte) renderOutput {
	return func() outcome.Result[[]byte] { return outcome.Ok(content) }
}

func renderedClientAndOpenAPI(api description.API, project projectfile.Project) outcome.Result[[]generatedFile] {
	return outcome.Fold(openapi.Render(api, project.OpenAPI().Version()), func(document []byte) outcome.Result[[]generatedFile] {
		files := []generatedFile{
			{path: project.OpenAPI().File(), content: outcome.Some(renderedOutput(document)), owner: openAPIOutput{}},
		}
		outcome.ForEachOption(project.TypeScriptFile(), func(path string) {
			files = append(files, generatedFile{
				path: path, owner: headedOutput{header: description.GeneratedHeader, source: "typescript"},
				content: outcome.Some(renderedOutput(effect.Render(api))),
			})
		})
		return outcome.Ok(files)
	}, outcome.Err[[]generatedFile])
}

func renderClaimedOutputs(outputs []claimedOutput, rendered []fileOnDisk, next finisher) Outcome {
	if len(outputs) == 0 {
		return next(rendered)
	}
	output := outputs[0]
	path := output.file.path
	renderWith := func(contents outcome.Option[[]byte]) Outcome {
		file := fileOnDisk{path: path, content: contents, held: output.held}
		return renderClaimedOutputs(outputs[1:], append(rendered, file), next)
	}
	return outcome.FoldOption(output.file.content, func(render renderOutput) Outcome {
		return outcome.Fold(render(), func(contents []byte) Outcome {
			return renderWith(outcome.Some(contents))
		}, func(cause error) Outcome { return FileNotWritten{Path: path, Cause: cause} })
	}, func() Outcome { return renderWith(outcome.None[[]byte]()) })
}
