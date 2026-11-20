package assembling

import (
	"github.com/CheckPickerUpper/nocontract/description"
	"github.com/CheckPickerUpper/nocontract/internal/keyed"
)

// FailureCodeTaken is an HTTP failure using a permanent code another response already owns.
//
//ergo:code NC-1139
//ergo:message Failure {Member} uses {Code}, which already identifies {First} declared at {FirstDeclared}.
type FailureCodeTaken struct {
	Code          string
	Member        string
	First         string
	FirstDeclared string
}

type failureCodeClaim struct {
	code     string
	member   string
	source   string
	position Position
}

func repeatedFailureCodes(members []DeclaredMember, shared []description.SharedFailure) []Refusal {
	claims := make([]failureCodeClaim, 0, len(shared)+len(members))
	for _, failure := range shared {
		claims = append(claims, failureCodeClaim{
			code: failure.Code.String(), member: failure.Name, source: "the NoContract runtime",
			position: Position{File: "", Line: 0, Column: 0},
		})
	}
	for _, declared := range members {
		switch answer := declared.Member.Answer.(type) {
		case description.Fails:
			claims = append(claims, failureCodeClaim{code: answer.Code.String(), member: declared.Member.Name, source: declared.Position.String(), position: declared.Position})
		case description.Succeeds, nil:
		}
	}
	refused := []Refusal{}
	for _, claim := range keyed.Repeats(claims, func(claim failureCodeClaim) string { return claim.code }) {
		refused = append(refused, Refusal{Position: claim.Item.position, Problem: FailureCodeTaken{
			Code: claim.Item.code, Member: claim.Item.member, First: claim.First.member, FirstDeclared: claim.First.source,
		}})
	}
	return refused
}
