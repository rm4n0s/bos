package command

import (
	"sort"

	"github.com/rm4n0s/errors"
)

// Restriction rule identifiers of SPEC §6.3.
const (
	RuleS1 = "S1"
	RuleS2 = "S2"
	RuleS3 = "S3"
	RuleS4 = "S4"
	RuleS5 = "S5"
	RuleS6 = "S6"
	RuleS7 = "S7"
	RuleS8 = "S8"
	RuleT1 = "T1"
	RuleT2 = "T2"
	RuleT3 = "T3"
	RuleT4 = "T4"
	RuleT5 = "T5"
	RuleR1 = "R1"
)

// Problem is a structured error entry of SPEC §15.3, so a client can underline
// the offending token.
type Problem struct {
	Rule     string `json:"rule"`
	Message  string `json:"message"`
	Position Pos    `json:"position"`
}

// ProblemOf extracts the structured problem from an error returned by Parse or
// FromJSON. It reports false for any other error.
func ProblemOf(err error) (Problem, bool) {
	appErr, ok := errors.FromError(err)
	if !ok {
		return Problem{}, false
	}
	rule, ok := appErr.Metadata["rule"].(string)
	if !ok {
		return Problem{}, false
	}
	pos, _ := appErr.Metadata["position"].(Pos)
	return Problem{Rule: rule, Message: appErr.Message, Position: pos}, true
}

// violation is a restriction failure that does not stop parsing: the parser
// collects them and reports the first by position once the syntax is known to
// be right.
type violation struct {
	rule  string
	msg   string
	at    Pos
	token string
}

var ruleOrder = map[string]int{
	RuleS1: 1, RuleS2: 2, RuleS3: 3, RuleS4: 4, RuleS5: 5, RuleS6: 6, RuleS7: 7, RuleS8: 8,
	RuleT1: 9, RuleT2: 10, RuleT3: 11, RuleT4: 12, RuleT5: 13,
}

// first returns the error for the violation that comes first in the text.
func firstViolation(vs []violation) error {
	if len(vs) == 0 {
		return nil
	}
	sort.SliceStable(vs, func(i, j int) bool {
		if vs[i].at.Offset != vs[j].at.Offset {
			return vs[i].at.Offset < vs[j].at.Offset
		}
		return ruleOrder[vs[i].rule] < ruleOrder[vs[j].rule]
	})
	v := vs[0]
	return restrictionError(v)
}

func restrictionError(v violation) *errors.Error {
	return errors.New("CommandRestrictionViolated", v.msg, "rule", v.rule, "position", v.at, "token", v.token)
}

func syntaxError(msg string, at Pos, token string) *errors.Error {
	return errors.New("CommandSyntaxError", msg, "rule", RuleSyntax, "position", at, "token", token)
}
