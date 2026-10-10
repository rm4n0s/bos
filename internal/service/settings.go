package service

import (
	"context"
	"slices"

	"github.com/rm4n0s/bos/internal/config"
	"github.com/rm4n0s/errors"
)

// SettingsService reads and changes the settings of the application, which are
// kept in the database (SPEC §2).
type SettingsService struct{ base }

// SettingsView is the settings page: every setting with its value (a default
// where nothing is stored), without the credentials.
type SettingsView struct {
	// Values are the settings by name, as the text the page shows. A credential
	// is empty here: SecretSet says whether it has a value.
	Values    map[string]string
	SecretSet map[string]bool
	// Overridden are the settings that the environment sets for this run; what
	// is saved for them takes effect when the environment stops setting them.
	Overridden map[string]bool
	// DBPath is the database file, where the default folders are.
	DBPath string
}

// SettingsInput is a change to the settings. A setting that is not named stays
// as it is. An empty value puts a setting back to its default (a folder to the
// one next to the database), except for a credential, which an empty value
// leaves alone: ClearSecrets removes those.
type SettingsInput struct {
	Values       map[string]string
	ClearSecrets []string
}

func knownSetting(key string) bool { return slices.Contains(config.Keys(), key) }

// Get returns the settings, as stored. The environment is not applied: it is
// for one run, and the page shows what a restart without it would use.
func (s *SettingsService) Get(ctx context.Context) (*SettingsView, error) {
	stored, err := s.Store.Repos().Settings().All(ctx)
	if err != nil {
		return nil, err
	}
	cfg, _, err := config.Resolve(stored, s.DBPath, nil)
	if err != nil {
		return nil, err
	}
	view := &SettingsView{Values: cfg.Values(), SecretSet: map[string]bool{}, Overridden: map[string]bool{}, DBPath: s.DBPath}
	for key := range view.Values {
		if config.IsSecret(key) {
			view.SecretSet[key] = view.Values[key] != ""
			view.Values[key] = ""
		}
	}
	for _, key := range s.Overridden {
		view.Overridden[key] = true
	}
	return view, nil
}

// Update checks the settings as they would be and, if they are valid, stores
// the change. Nothing is stored when any of them is not.
func (s *SettingsService) Update(ctx context.Context, in SettingsInput) error {
	for key := range in.Values {
		if !knownSetting(key) {
			return errors.New("InvalidRequest", "there is no such setting", "setting", key)
		}
	}
	for _, key := range in.ClearSecrets {
		if !config.IsSecret(key) {
			return errors.New("InvalidRequest", "only a credential can be cleared", "setting", key)
		}
	}
	repo := s.Store.Repos().Settings()
	stored, err := repo.All(ctx)
	if err != nil {
		return err
	}
	change := map[string]string{}
	for key, value := range in.Values {
		if config.IsSecret(key) && value == "" {
			continue // left empty: not changed
		}
		change[key] = value
	}
	for _, key := range in.ClearSecrets {
		change[key] = ""
	}
	merged := map[string]string{}
	for key, value := range stored {
		merged[key] = value
	}
	for key, value := range change {
		if value == "" {
			delete(merged, key)
		} else {
			merged[key] = value
		}
	}
	if _, _, err := config.Resolve(merged, s.DBPath, nil); err != nil {
		return err
	}
	return repo.Save(ctx, change)
}
