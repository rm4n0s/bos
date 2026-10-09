package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// TagService lists, renames and merges tags (SPEC §3.4).
type TagService struct{ base }

func scopeOf(scope domain.TagScope) error {
	if scope != domain.TagScopeGroup && scope != domain.TagScopeItem {
		return errors.New("InvalidRequest", "the tag scope must be group or item", "scope", string(scope))
	}
	return nil
}

// List returns the tags in use in a scope with their counts, for autocomplete.
// A prefix limits the list; case is ignored.
func (s *TagService) List(ctx context.Context, scope domain.TagScope, prefix string) ([]domain.TagCount, error) {
	if err := scopeOf(scope); err != nil {
		return nil, err
	}
	return s.Store.Repos().Tags().List(ctx, scope, strings.ToLower(prefix))
}

// RenameResult is the outcome of a rename.
type RenameResult struct {
	// Renamed is how many groups or items carried the old tag.
	Renamed int `json:"renamed"`
	// AffectedCommands are the commands whose text mentions the old item tag;
	// they fail validation on their next edit or re-creation (SPEC §3.4). It is
	// empty for group tags.
	AffectedCommands []domain.Command `json:"affected_commands"`
}

// Rename changes a tag on every group or item that carries it; a new name that
// is already in use merges the two. Renaming an item tag does not rewrite the
// commands: the result lists those that mention the old tag.
func (s *TagService) Rename(ctx context.Context, scope domain.TagScope, from, to string) (*RenameResult, error) {
	if err := scopeOf(scope); err != nil {
		return nil, err
	}
	from, to = strings.ToLower(from), strings.ToLower(to)
	if err := checkTag(from); err != nil {
		return nil, err
	}
	if err := checkTag(to); err != nil {
		return nil, err
	}
	r := s.Store.Repos()
	n, err := r.Tags().Rename(ctx, scope, from, to)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errors.New("TagNotFound", "no "+string(scope)+" carries this tag", "tag", from, "scope", string(scope))
	}
	res := &RenameResult{Renamed: n, AffectedCommands: []domain.Command{}}
	if scope == domain.TagScopeItem && from != to {
		found, err := r.Commands().FindByText(ctx, "#"+from)
		if err != nil {
			return nil, err
		}
		// The search is a substring search: #combat also matches #combatant.
		mention := regexp.MustCompile(`(?i)#` + regexp.QuoteMeta(from) + `([^A-Za-z0-9_]|$)`)
		for _, c := range found {
			if mention.MatchString(c.CommandCode) {
				res.AffectedCommands = append(res.AffectedCommands, c)
			}
		}
	}
	return res, nil
}
