package reading

import (
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/tools/go/packages"

	"github.com/CheckPickerUpper/nocontract/go/serverplan"
)

// serverLoaded is the project package in the server folder, absent when the folder holds no Go
// file the project wrote among the packages loaded.
func serverLoaded(index projectIndex) outcome.Option[*packages.Package] {
	for _, loaded := range index.project {
		if loaded.Dir == index.request.Server {
			return outcome.Some(loaded)
		}
	}
	return outcome.None[*packages.Package]()
}

// plannedServer is the server package NoContract writes: named as the package the project wrote in
// its folder, or for the folder when it holds none.
func plannedServer(index projectIndex) serverplan.Server {
	return outcome.FoldOption(serverLoaded(index), func(loaded *packages.Package) serverplan.Server {
		home := serverplan.ServerBesideProjectCode{Path: loaded.PkgPath, Declared: loaded.Types.Scope().Names()}
		return serverplan.Server{Directory: index.request.Server, Name: loaded.Name, Home: home}
	}, func() serverplan.Server {
		return serverplan.Server{Directory: index.request.Server, Name: folderPackageName(index.request.Server), Home: serverplan.ServerAlone{}}
	})
}

// folderPackageName is the name of a package written into an empty folder: the folder's name,
// each character an identifier cannot hold written as an underscore, lower case.
func folderPackageName(directory string) string {
	cleaned := strings.Map(func(letter rune) rune {
		if unicode.IsLetter(letter) || unicode.IsDigit(letter) {
			return unicode.ToLower(letter)
		}
		return '_'
	}, filepath.Base(directory))
	if !token.IsIdentifier(cleaned) {
		return "server_" + cleaned
	}
	return cleaned
}

// serverNameChecks refuses each name the server file declares that a file the project wrote in the
// server folder already declares.
func serverNameChecks(index projectIndex) found {
	return outcome.FoldOption(serverLoaded(index), func(loaded *packages.Package) found {
		finding := nothingFound()
		for _, name := range serverplan.ServerNames() {
			outcome.ForEachOption(outcome.EmptyableToOption(loaded.Types.Scope().Lookup(name)), func(declared types.Object) {
				finding = finding.with(index.refused(declared.Pos(), GeneratedNameTaken{Name: name}))
			})
		}
		return finding
	}, nothingFound)
}
