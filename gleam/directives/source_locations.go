package directives

import (
	"regexp"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/CheckPickerUpper/nocontract/assembling"
)

type sourceLocations struct {
	file         string
	functions    map[string]assembling.Position
	constructors map[string]assembling.Position
}

// The compiler owns declaration attachment. These tokens locate its symbols in the source.
var sourceTokens = regexp.MustCompile("(?s)//[^\\n]*|\"(?:\\\\.|[^\"\\\\])*\"|[_\\pL][_\\pL\\pN]*|[{}()=/.]")

func locateDeclarations(file, source string) sourceLocations {
	locations := sourceLocations{file: file, functions: map[string]assembling.Position{}, constructors: map[string]assembling.Position{}}
	lines := []int{0}
	for offset, character := range source {
		if character == '\n' {
			lines = append(lines, offset+1)
		}
	}
	braces, parentheses := 0, 0
	publicType := false
	previous, beforePrevious := "", ""
	for _, span := range sourceTokens.FindAllStringIndex(source, -1) {
		token := source[span[0]:span[1]]
		switch token[0] {
		case '/', '"':
			continue
		}
		switch token {
		case "type":
			publicType = braces == 0 && (previous == "pub" || previous == "opaque")
		case "=":
			if braces == 0 && parentheses == 0 {
				publicType = false
			}
		case "{":
			braces++
		case "}":
			braces--
			if braces == 0 {
				publicType = false
			}
		case "(":
			parentheses++
		case ")":
			parentheses--
		default:
			if braces == 0 && parentheses == 0 && previous == "fn" && beforePrevious == "pub" {
				locations.functions[token] = sourceLocation(file, lines, span[0])
			}
			if publicType && braces == 1 && parentheses == 0 {
				initial, _ := utf8.DecodeRuneInString(token)
				if unicode.IsUpper(initial) {
					locations.constructors[token] = sourceLocation(file, lines, span[0])
				}
			}
		}
		beforePrevious, previous = previous, token
	}
	return locations
}

func sourceLocation(file string, lines []int, offset int) assembling.Position {
	line, startsLine := slices.BinarySearch(lines, offset)
	if !startsLine {
		line--
	}
	return assembling.Position{File: file, Line: line + 1, Column: offset - lines[line] + 1}
}

func (locations sourceLocations) position(index map[string]assembling.Position, name string) assembling.Position {
	if position, located := index[name]; located {
		return position
	}
	return assembling.Position{File: locations.file, Line: 0, Column: 0}
}
