package service

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/mask"
	"github.com/rm4n0s/errors"
)

// ItemService manages items and their item tags (SPEC §3.6).
type ItemService struct{ base }

// controlNetTypes are the ControlNet types of SPEC §3.6.
var controlNetTypes = []string{"openpose", "dwpose", "tile", "canny", "midas", "depth", "lineart", "softedge", "scribble", "normal"}

func (s *ItemService) check(v *domain.Item) error {
	if v.Name != "" {
		if err := checkName("item", v.Name, maxName); err != nil {
			return err
		}
	}
	if err := checkCodeName("item", v.CodeName); err != nil {
		return err
	}
	if v.OutputType != "" && !v.OutputType.Valid() {
		return errors.New("OutputTypeInvalid", "the output type must be image, video, 3d or unknown", "output_type", string(v.OutputType))
	}
	tags, err := cleanTags(v.Tags)
	if err != nil {
		return err
	}
	v.Tags = tags
	for _, c := range v.ControlNets {
		if !slices.Contains(controlNetTypes, c.Type) {
			return errors.New("ItemControlNetInvalid", "unknown ControlNet type", "type", c.Type, "valid", controlNetTypes)
		}
	}
	return checkRegions(v)
}

// checkRegions verifies the region keywords of an item: each is a command word,
// and none is defined twice, by mask or by coordinates (SPEC §6.6).
func checkRegions(v *domain.Item) error {
	seen := map[string]bool{}
	use := func(keyword string) error {
		if msg := checkKeyword(keyword); msg != "" {
			return errors.New("ItemRegionInvalid", msg, "keyword", keyword)
		}
		if seen[keyword] {
			return errors.New("ItemRegionInvalid", "a region keyword is defined twice on the item", "keyword", keyword)
		}
		seen[keyword] = true
		return nil
	}
	if v.MaskRegion != nil {
		for _, k := range sortedKeys(v.MaskRegion.MaskFiles) {
			if err := use(k); err != nil {
				return err
			}
		}
	}
	for _, c := range v.CoordinatedRegions {
		if err := use(c.Keyword); err != nil {
			return err
		}
		if c.Width < 1 || c.Height < 1 || c.X < 0 || c.Y < 0 {
			return errors.New("ItemRegionInvalid", "a region needs a positive size and a position that is not negative", "keyword", c.Keyword)
		}
	}
	return nil
}

func checkKeyword(k string) string {
	if k == "" {
		return "a region keyword must not be empty"
	}
	return commandNameProblem(k)
}

// maskRegion generates the masks of a reference image that has none yet
// (SPEC §7.3). It returns the folder it created, for removal if the item then
// fails to save.
func (s *ItemService) maskRegion(ctx context.Context, v *domain.Item, old *domain.Item) (created string, err error) {
	m := v.MaskRegion
	if m == nil || m.ReferencePath == "" {
		return "", nil
	}
	if old != nil && old.MaskRegion != nil && old.MaskRegion.ReferencePath == m.ReferencePath && old.MaskRegion.FolderPath != "" {
		// The same reference image: keep the masks already made.
		m.FolderPath, m.MaskFiles = old.MaskRegion.FolderPath, old.MaskRegion.MaskFiles
		return "", nil
	}
	if err := ctx.Err(); err != nil {
		return "", errors.NewErr("MaskGenerationFailed", err).SetMetadata("reason", "cancelled")
	}
	r, err := s.Files.Open(m.ReferencePath)
	if err != nil {
		return "", err
	}
	masks, genErr := mask.Generate(r)
	if closeErr := r.Close(); genErr == nil && closeErr != nil {
		genErr = errors.NewErr("FileStoreFailure", closeErr).SetMetadata("path", m.ReferencePath, "operation", "close")
	}
	if genErr != nil {
		return "", genErr
	}
	folder := filepath.Join(s.Dirs.Regions, "item-"+strconv.FormatUint(s.NewSeed(), 16))
	files, err := mask.Save(folder, masks)
	if err != nil {
		s.removeFiles([]string{folder})
		return "", err
	}
	m.FolderPath, m.MaskFiles = folder, files
	return folder, nil
}

// Create adds an item to a group. A mask reference image without masks gets
// its masks generated (SPEC §7.3).
func (s *ItemService) Create(ctx context.Context, group domain.ID, in domain.Item) (*domain.Item, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	in.ID, in.GroupID = 0, group
	if in.MaskRegion != nil {
		m := *in.MaskRegion
		in.MaskRegion = &m
	}
	folder, err := s.maskRegion(ctx, &in, nil)
	if err != nil {
		return nil, err
	}
	// The masks may add keywords, so the item is checked once more.
	if err := checkRegions(&in); err != nil {
		s.removeFiles([]string{folder})
		return nil, err
	}
	items := s.Store.Repos().Items()
	if err := items.Create(ctx, &in); err != nil {
		s.removeFiles([]string{folder})
		return nil, err
	}
	return items.Get(ctx, in.ID)
}

// Update replaces the editable fields of an item, including its tags. Files the
// edit no longer refers to are removed.
func (s *ItemService) Update(ctx context.Context, id domain.ID, in domain.Item) (*domain.Item, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	items := s.Store.Repos().Items()
	old, err := items.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	in.ID, in.GroupID = id, old.GroupID
	if in.MaskRegion != nil {
		m := *in.MaskRegion
		in.MaskRegion = &m
	}
	folder, err := s.maskRegion(ctx, &in, old)
	if err != nil {
		return nil, err
	}
	if err := checkRegions(&in); err != nil {
		s.removeFiles([]string{folder})
		return nil, err
	}
	if err := items.Update(ctx, &in); err != nil {
		s.removeFiles([]string{folder})
		return nil, err
	}
	kept := in.Files()
	var stale []string
	for _, f := range old.Files() {
		if !slices.Contains(kept, f) {
			stale = append(stale, f)
		}
	}
	s.removeFiles(stale)
	return items.Get(ctx, id)
}

// Delete removes an item and its files (SPEC §7.4).
func (s *ItemService) Delete(ctx context.Context, id domain.ID) error {
	files, err := s.Store.Repos().Items().Delete(ctx, id)
	if err != nil {
		return err
	}
	s.removeFiles(files)
	return nil
}

// Get returns one item with its tags.
func (s *ItemService) Get(ctx context.Context, id domain.ID) (*domain.Item, error) {
	return s.Store.Repos().Items().Get(ctx, id)
}

// ListByGroup returns one page of a group's items in creation order. It fails
// with GroupNotFound for a group that does not exist.
func (s *ItemService) ListByGroup(ctx context.Context, group domain.ID, p domain.Page) (domain.Paged[domain.Item], error) {
	r := s.Store.Repos()
	if _, err := r.Groups().Get(ctx, group); err != nil {
		return domain.Paged[domain.Item]{}, err
	}
	return r.Items().ListByGroup(ctx, group, p)
}
