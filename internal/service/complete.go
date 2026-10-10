package service

import (
	"context"
	"sort"
	"strings"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// completeLimit is how many names one completion lists at most.
const completeLimit = 12

// Completions are the code names that fit at the cursor of a command being
// written.
type Completions struct {
	Kind command.CompletionKind
	// Start and End are the byte offsets of the text a chosen name replaces.
	Start, End int
	// Names are sorted, at most completeLimit of them; More says there were more.
	Names []string
	More  bool
}

// Complete lists the servers, generators, fixers, groups, items or item tags
// that fit at the cursor (a byte offset) of the command text, for the editor's
// autocomplete. It stores nothing, and text that does not parse is no error:
// where no name fits the list is empty and Kind is command.CompleteNone.
func (s *CommandService) Complete(ctx context.Context, code string, cursor int) (*Completions, error) {
	at := command.CompletionAt(code, cursor)
	out := &Completions{Kind: at.Kind, Start: at.Start, End: at.End, Names: []string{}}
	if at.Kind == command.CompleteNone {
		return out, nil
	}
	names, err := s.namesFor(ctx, at)
	if err != nil {
		return nil, err
	}
	prefix := strings.ToLower(at.Prefix)
	for _, n := range names {
		if strings.HasPrefix(strings.ToLower(n), prefix) {
			out.Names = append(out.Names, n)
		}
	}
	sort.Slice(out.Names, func(i, j int) bool { return strings.ToLower(out.Names[i]) < strings.ToLower(out.Names[j]) })
	// A name that is already complete has nothing left to offer.
	if len(out.Names) == 1 && out.Names[0] == at.Prefix {
		out.Names = []string{}
	}
	if len(out.Names) > completeLimit {
		out.Names, out.More = out.Names[:completeLimit], true
	}
	return out, nil
}

// namesFor returns every name of the kind the completion asks for.
func (s *CommandService) namesFor(ctx context.Context, at command.Completion) ([]string, error) {
	r := s.Store.Repos()
	switch at.Kind {
	case command.CompleteServer:
		all, err := r.Servers().All(ctx)
		return codes(all, func(v domain.Server) string { return v.CodeName }), err
	case command.CompleteGenerator:
		all, err := allPages(func(p domain.Page) (domain.Paged[domain.Generator], error) { return r.Generators().List(ctx, p) })
		return codes(all, func(v domain.Generator) string { return v.CodeName }), err
	case command.CompleteFixer:
		all, err := allPages(func(p domain.Page) (domain.Paged[domain.Fixer], error) { return r.Fixers().List(ctx, p) })
		return codes(all, func(v domain.Fixer) string { return v.CodeName }), err
	case command.CompleteGroup:
		all, err := allPages(func(p domain.Page) (domain.Paged[domain.Group], error) { return r.Groups().List(ctx, "", p) })
		return codes(all, func(v domain.Group) string { return v.CodeName }), err
	case command.CompleteItem:
		g, err := r.Groups().GetByCode(ctx, at.Group)
		if err != nil {
			if appErr, ok := errors.FromError(err); ok && appErr.Tag == "GroupNotFound" {
				return nil, nil // a group that is not there offers nothing
			}
			return nil, err
		}
		items, err := r.Items().AllByGroup(ctx, g.ID)
		return codes(items, func(v domain.Item) string { return v.CodeName }), err
	case command.CompleteTag:
		tags, err := r.Tags().List(ctx, domain.TagScopeItem, "")
		return codes(tags, func(v domain.TagCount) string { return v.Tag }), err
	case command.CompleteNone:
	}
	return nil, nil
}

func codes[T any](in []T, name func(T) string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = name(v)
	}
	return out
}

// allPages reads a paged list to its end.
func allPages[T any](list func(domain.Page) (domain.Paged[T], error)) ([]T, error) {
	var all []T
	for n := 1; ; n++ {
		page, err := list(domain.Page{Number: n, Size: domain.MaxPageSize})
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if n >= page.TotalPages {
			return all, nil
		}
	}
}
