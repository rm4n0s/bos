package service

import (
	"sort"

	"github.com/rm4n0s/bos/internal/command"
)

// commandNameProblem says why a word cannot be used in a command, or returns
// the empty string.
func commandNameProblem(word string) string { return command.CheckName(word) }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
