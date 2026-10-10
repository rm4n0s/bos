package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/bos/internal/civitai"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/events"
	"github.com/rm4n0s/errors"
)

// maxImportModels bounds one import, so a typo cannot start an endless download.
const maxImportModels = 100

// CivitaiService imports LoRAs from Civitai into groups (SPEC §9). A download
// takes long and Civitai asks for politeness, so an import returns at once and
// goes on in the background, one at a time; its progress is told through the
// event log.
type CivitaiService struct {
	base
	groups *GroupService
	items  *ItemService

	sem chan struct{}
	wg  sync.WaitGroup
}

// CivitaiModel is one model to import with the strengths its LoRA entry gets.
// VersionID, when not 0, picks that version of the model instead of the newest.
type CivitaiModel struct {
	ModelID       int
	VersionID     int
	StrengthModel float64
	StrengthClip  float64
}

// label names the model in a message: `7`, or `7/456` with a version.
func (m CivitaiModel) label() string {
	if m.VersionID > 0 {
		return strconv.Itoa(m.ModelID) + "/" + strconv.Itoa(m.VersionID)
	}
	return strconv.Itoa(m.ModelID)
}

// codeName is the code name of the item: the model id, followed by the
// version id when one was asked for, so two versions of a model can both be in
// a group. Only letters, digits and underscores make a name a command can use.
func (m CivitaiModel) codeName(id int) string {
	if m.VersionID > 0 {
		return strconv.Itoa(id) + "_" + strconv.Itoa(m.VersionID)
	}
	return strconv.Itoa(id)
}

// CivitaiImport is the request to import models.
type CivitaiImport struct {
	// Group is the group to create (ImportGroup); its flag for LoRAs is set.
	Group  domain.Group
	Models []CivitaiModel
	// ComfyPath is the folder of the ComfyUI installation; the models go to
	// <ComfyPath>/models/loras/<Subfolder>.
	ComfyPath string
	Subfolder string
}

// Wait blocks until the imports that are running have ended. Shutdown and tests
// use it.
func (s *CivitaiService) Wait() { s.wg.Wait() }

func (s *CivitaiService) check(in *CivitaiImport) error {
	if s.Civitai == nil || !s.Civitai.Configured() || s.Dirs.CivitaiCache == "" {
		return errors.New("CivitaiNotConfigured", "Civitai needs an API token (civitai_token, set in Settings → Civitai) and a cache folder (civitai_cache_dir)")
	}
	if len(in.Models) == 0 {
		return errors.New("InvalidRequest", "at least one model is needed", "field", "models")
	}
	if len(in.Models) > maxImportModels {
		return errors.New("InvalidRequest", "too many models in one import", "limit", maxImportModels)
	}
	for _, m := range in.Models {
		if m.ModelID < 1 {
			return errors.New("InvalidRequest", "a model id is a positive number", "model_id", m.ModelID)
		}
		if m.VersionID < 0 {
			return errors.New("InvalidRequest", "a version id is a positive number", "model_id", m.ModelID, "version_id", m.VersionID)
		}
	}
	_, err := civitai.CheckTarget(in.ComfyPath, in.Subfolder)
	return err
}

// ImportGroup creates a group and imports one item into it for every model:
// code name the model id, name the model's name, the trigger words as the
// positive prompt, and one LoRA entry (SPEC §9). It returns the new, still
// empty group; the items arrive as the downloads finish.
func (s *CivitaiService) ImportGroup(ctx context.Context, in CivitaiImport) (*domain.Group, error) {
	if err := s.check(&in); err != nil {
		return nil, err
	}
	in.Group.UseLora = true
	g, err := s.groups.Create(ctx, in.Group)
	if err != nil {
		return nil, err
	}
	s.start(g.ID, in)
	return g, nil
}

// AddItem imports one model as an item of an existing group.
func (s *CivitaiService) AddItem(ctx context.Context, group domain.ID, m CivitaiModel, comfyPath, subfolder string) (*domain.Group, error) {
	in := CivitaiImport{Models: []CivitaiModel{m}, ComfyPath: comfyPath, Subfolder: subfolder}
	if err := s.check(&in); err != nil {
		return nil, err
	}
	g, err := s.groups.Get(ctx, group)
	if err != nil {
		return nil, err
	}
	s.start(g.ID, in)
	return g, nil
}

// start runs an import in the background.
func (s *CivitaiService) start(group domain.ID, in CivitaiImport) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx := s.Background
		if ctx == nil {
			ctx = context.Background()
		}
		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		case <-ctx.Done():
			return
		}
		s.run(ctx, group, in)
	}()
}

func (s *CivitaiService) run(ctx context.Context, group domain.ID, in CivitaiImport) {
	done, failed := 0, 0
	pause := false
	for i, m := range in.Models {
		if pause {
			if err := s.sleep(ctx, s.Pause); err != nil {
				return
			}
		}
		item, skipped, err := s.importOne(ctx, group, m, in)
		pause = !skipped
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			failed++
			logError(s.Logger, "a Civitai import failed", err, "model_id", m.ModelID, "version_id", m.VersionID)
			s.emit(events.ImportError, group, "Model "+m.label()+": "+userMessage(err))
		default:
			done++
			s.emit(events.ImportProgress, group, "Imported "+item.Name+" ("+strconv.Itoa(i+1)+"/"+strconv.Itoa(len(in.Models))+").")
		}
	}
	msg := strconv.Itoa(done) + " of " + strconv.Itoa(len(in.Models)) + " models imported."
	if failed > 0 {
		msg += " " + strconv.Itoa(failed) + " failed."
	}
	s.emit(events.ImportFinished, group, msg)
}

// importOne downloads a model, installs it and adds its item. skipped is true
// when the model was already in the cache, so no pause is needed before the next.
func (s *CivitaiService) importOne(ctx context.Context, group domain.ID, m CivitaiModel, in CivitaiImport) (item *domain.Item, skipped bool, err error) {
	model, err := s.Civitai.Download(ctx, m.ModelID, m.VersionID, s.Dirs.CivitaiCache)
	if err != nil {
		return nil, false, err
	}
	name, err := s.Civitai.Install(model, in.ComfyPath, in.Subfolder)
	if err != nil {
		return nil, model.Skipped, err
	}
	title := strings.TrimSpace(model.Name)
	if title == "" {
		title = "Model " + strconv.Itoa(model.ID)
	}
	if utf8.RuneCountInString(title) > maxName {
		title = string([]rune(title)[:maxName])
	}
	item, err = s.items.Create(ctx, group, domain.Item{
		CodeName: m.codeName(model.ID), Name: title,
		PositivePrompt: strings.Join(model.TriggerWords, "\n\n"),
		LoRAs:          []domain.LoRA{{Name: name, StrengthModel: m.StrengthModel, StrengthClip: m.StrengthClip}},
	})
	return item, model.Skipped, err
}

func (s *CivitaiService) emit(t events.Type, group domain.ID, message string) {
	if s.Events != nil {
		s.Events.Append(events.Event{Type: t, GroupID: group, Message: message})
	}
}

func (s *CivitaiService) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// userMessage is the sentence an event carries for a failure.
func userMessage(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return "unexpected failure"
}
