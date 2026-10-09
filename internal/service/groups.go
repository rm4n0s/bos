package service

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
)

// GroupService manages groups and their group tags (SPEC §3.5).
type GroupService struct{ base }

func (s *GroupService) check(v *domain.Group) error {
	if err := checkName("group", v.Name, maxName); err != nil {
		return err
	}
	if err := checkCodeName("group", v.CodeName); err != nil {
		return err
	}
	tags, err := cleanTags(v.Tags)
	if err != nil {
		return err
	}
	v.Tags = tags
	return nil
}

// Create adds a group. Tags are lower-cased and checked (SPEC §3.4).
func (s *GroupService) Create(ctx context.Context, in domain.Group) (*domain.Group, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	in.ID = 0
	if err := s.Store.Repos().Groups().Create(ctx, &in); err != nil {
		return nil, err
	}
	return &in, nil
}

// Update replaces the editable fields of a group, including its tags. A
// thumbnail that the edit replaced is removed.
func (s *GroupService) Update(ctx context.Context, id domain.ID, in domain.Group) (*domain.Group, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	in.ID = id
	groups := s.Store.Repos().Groups()
	old, err := groups.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := groups.Update(ctx, &in); err != nil {
		return nil, err
	}
	if old.Thumbnail != "" && old.Thumbnail != in.Thumbnail {
		s.removeFiles([]string{old.Thumbnail})
	}
	return groups.Get(ctx, id)
}

// Delete removes a group together with its items and their files (SPEC §7.4).
func (s *GroupService) Delete(ctx context.Context, id domain.ID) error {
	files, err := s.Store.Repos().Groups().Delete(ctx, id)
	if err != nil {
		return err
	}
	s.removeFiles(files)
	return nil
}

// Get returns one group with its tags.
func (s *GroupService) Get(ctx context.Context, id domain.ID) (*domain.Group, error) {
	return s.Store.Repos().Groups().Get(ctx, id)
}

// List returns one page of groups; a non-empty tag keeps the groups that carry
// that group tag.
func (s *GroupService) List(ctx context.Context, tag string, p domain.Page) (domain.Paged[domain.Group], error) {
	return s.Store.Repos().Groups().List(ctx, strings.ToLower(tag), p)
}

var templateTag = regexp.MustCompile(`<tmpl:([^>]+)>`)

// ImportText creates a group with one item per non-blank line of text
// (SPEC §3.6): the code name and name of an item are the index of its line,
// counted from 0; its positive prompt is the line; its negative prompt holds
// one <tmpl:tag> marker for every tag the line has. The group and its items
// are created together or not at all.
func (s *GroupService) ImportText(ctx context.Context, in domain.Group, text string) (*domain.Group, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	in.ID = 0
	err := s.Store.InTx(ctx, func(r Repos) error {
		if err := r.Groups().Create(ctx, &in); err != nil {
			return err
		}
		for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var markers []string
			for _, m := range templateTag.FindAllStringSubmatch(line, -1) {
				markers = append(markers, "<tmpl:"+m[1]+">")
			}
			it := domain.Item{
				GroupID: in.ID, CodeName: strconv.Itoa(i), Name: strconv.Itoa(i),
				PositivePrompt: line, NegativePrompt: strings.Join(markers, ","),
			}
			if err := r.Items().Create(ctx, &it); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Store.Repos().Groups().Get(ctx, in.ID)
}
