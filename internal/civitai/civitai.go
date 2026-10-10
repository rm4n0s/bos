// Package civitai is the Civitai port and its adapter over gocivitailib
// (SPEC §9, §19). It downloads models into the cache and installs them into a
// ComfyUI installation.
package civitai

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/gocivitailib"
)

// Models is what the application needs from Civitai (SPEC §19.3).
type Models interface {
	// Download stores <dir>/<modelID>.safetensors and <dir>/<modelID>.json.
	Download(ctx context.Context, modelID int, dir string) (Model, error)
	// Install copies a downloaded model into the LoRA folder of a ComfyUI
	// installation, below the sub-folder, and returns the name ComfyUI knows
	// it by: `<subfolder>/<id>.safetensors`.
	Install(m Model, comfyPath, subfolder string) (name string, err error)
	// Configured reports whether downloads can be attempted.
	Configured() bool
}

// Model is a downloaded model.
type Model struct {
	ID           int
	VersionID    int
	Name         string
	BaseModel    string
	TriggerWords []string
	Tags         []string
	Description  string
	FilePath     string
	MetadataPath string
	Size         int64
	// Skipped is true when the file was already in the cache.
	Skipped bool
}

// Options configure the adapter.
type Options struct {
	// Token and Domain come from the configuration; the environment is never
	// consulted (SPEC §19.4).
	Token  string
	Domain string
	// SiteURL replaces the site address derived from the domain; tests use it.
	SiteURL string
	Logger  *slog.Logger
	// MaxRetries and RetryBackoff tune the library's retries of a GET; tests
	// make them quick. Zero keeps the library's defaults.
	MaxRetries   int
	RetryBackoff time.Duration
}

// Adapter is Models over gocivitailib.
type Adapter struct {
	client *gocivitailib.Client
	token  string
}

// New builds the adapter.
func New(o Options) *Adapter {
	opts := []gocivitailib.Option{gocivitailib.WithToken(o.Token), gocivitailib.WithDomain(o.Domain)}
	if o.SiteURL != "" {
		opts = append(opts, gocivitailib.WithSiteURL(o.SiteURL))
	}
	if o.Logger != nil {
		opts = append(opts, gocivitailib.WithLogger(o.Logger))
	}
	if o.MaxRetries != 0 {
		opts = append(opts, gocivitailib.WithMaxRetries(o.MaxRetries))
	}
	if o.RetryBackoff > 0 {
		opts = append(opts, gocivitailib.WithRetryBackoff(o.RetryBackoff, o.RetryBackoff))
	}
	return &Adapter{client: gocivitailib.NewClient(opts...), token: o.Token}
}

// Configured reports whether a token is set.
func (a *Adapter) Configured() bool { return a.token != "" }

// Download fetches a model into dir. Without a token nothing is attempted.
func (a *Adapter) Download(ctx context.Context, modelID int, dir string) (Model, error) {
	if !a.Configured() || dir == "" {
		return Model{}, notConfigured()
	}
	d, err := a.client.DownloadModel(ctx, modelID, dir)
	if err != nil {
		return Model{}, mapError(err, modelID)
	}
	m := Model{
		ID: modelID, FilePath: d.ModelPath, MetadataPath: d.MetadataPath, Size: d.Size, Skipped: d.Skipped,
	}
	if d.Info != nil {
		m.Name, m.Tags, m.Description = d.Info.Name, d.Info.Tags, d.Info.Description
	}
	m.VersionID, m.BaseModel, m.TriggerWords = d.Version.ID, d.Version.BaseModel, d.Version.TrainedWords
	return m, nil
}

func notConfigured() error {
	return errors.New("CivitaiNotConfigured", "Civitai needs an API token (civitai_token, set in Settings → Civitai) and a cache folder (civitai_cache_dir)")
}

// mapError turns the library's errors into the tags of SPEC §19.4.
func mapError(err error, modelID int) error {
	var limited *gocivitailib.RateLimitedError
	var status *gocivitailib.StatusError
	switch {
	case errors.Is(err, gocivitailib.ErrMissingToken):
		return notConfigured()
	case errors.As(err, &limited):
		return errors.NewErr("CivitaiRateLimited", err).SetMetadata("model_id", modelID, "retry_after", limited.RetryAfter.String())
	case errors.As(err, &status) && status.Status == 404:
		return errors.NewErr("CivitaiModelNotFound", err).SetMetadata("model_id", modelID)
	case errors.As(err, &status):
		return errors.NewErr("CivitaiDownloadFailed", err).SetMetadata("model_id", modelID, "status", status.Status, "reason", err.Error())
	case errors.Is(err, gocivitailib.ErrHashMismatch), errors.Is(err, gocivitailib.ErrNoDownloadableFile):
		return errors.NewErr("CivitaiDownloadFailed", err).SetMetadata("model_id", modelID, "reason", err.Error())
	case errors.Is(err, gocivitailib.ErrInvalidResponse):
		return errors.NewErr("CivitaiMetadataInvalid", err).SetMetadata("model_id", modelID)
	}
	// Anything else (the network, the disk, a cancelled context) is a failed download.
	return errors.NewErr("CivitaiDownloadFailed", err).SetMetadata("model_id", modelID, "reason", err.Error())
}

// ModelName is the name ComfyUI knows a model by below a sub-folder.
func ModelName(modelID int, subfolder string) string {
	file := strconv.Itoa(modelID) + ".safetensors"
	if subfolder == "" {
		return file
	}
	return subfolder + "/" + file
}
