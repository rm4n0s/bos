package ui

import (
	"net/http"
	"strings"

	"github.com/rm4n0s/bos/internal/config"
	"github.com/rm4n0s/bos/internal/service"
	"github.com/rm4n0s/errors"
)

// settingField is one input of the settings page.
type settingField struct {
	Key, Label, Help string
	// Kind is text, secret or select.
	Kind        string
	Placeholder string
	Options     []string
	Value       string
	// Set tells, for a secret, whether it has a value (the value is never shown).
	Set bool
	// Override is the environment variable that sets this setting for this run.
	Override string
}

type settingSection struct {
	Title, Intro string
	Fields       []settingField
}

// settingsPage is what the settings page is drawn from.
type settingsPage struct {
	Sections   []settingSection
	DBPath     string
	CanRestart bool
	// Saved is true after a save: the page then says to restart.
	Saved bool
}

// settingsLayout is the order, wording and kind of every setting. A test fails
// when a setting of the configuration has no field here.
var settingsLayout = []settingSection{
	{Title: "Folders", Intro: "Where the program keeps its files. A folder left empty is the default one, next to the database.", Fields: []settingField{
		{Key: "results_dir", Label: "Results", Help: "Pictures and other results of the jobs."},
		{Key: "controlnet_dir", Label: "ControlNet images", Help: "Images uploaded for ControlNet."},
		{Key: "ipadapter_dir", Label: "IP-Adapter images", Help: "Images uploaded for IP-Adapter."},
		{Key: "regions_dir", Label: "Region masks", Help: "Region reference images and the masks made from them."},
		{Key: "thumbnails_dir", Label: "Thumbnails", Help: "Pictures of groups and items."},
		{Key: "downloads_dir", Label: "Downloads", Help: "The ZIP archives of finished commands."},
		{Key: "civitai_cache_dir", Label: "Civitai cache", Help: "Where LoRAs are kept after they are downloaded from Civitai."},
	}},
	{Title: "Civitai", Intro: "Needed to import LoRAs from Civitai.", Fields: []settingField{
		{Key: "civitai_token", Label: "API key", Kind: "secret", Help: "Your Civitai API key, from your Civitai account settings."},
		{Key: "civitai_domain", Label: "Domain", Placeholder: config.DefaultCivitaiDomain, Help: "Change it only if your account lives on another Civitai domain."},
		{Key: "civitai_pause_seconds", Label: "Pause between downloads (seconds)", Placeholder: "15", Help: "How long to wait between two downloads of one import."},
	}},
	{Title: "Server", Intro: "How this program can be reached.", Fields: []settingField{
		{Key: "listen_addr", Label: "Listen address", Placeholder: config.DefaultListenAddr, Help: "host:port. Keep 127.0.0.1 unless other machines must reach it; then set an access token too."},
		{Key: "auth_token", Label: "Access token", Kind: "secret", Help: "If set, the web page asks for it once and the API needs it as a bearer token."},
		{Key: "cors_allow_list", Label: "Allowed origins", Placeholder: "http://localhost:3000", Help: "Other websites that may call the API from a browser, separated by commas."},
		{Key: "upload_limit_bytes", Label: "Largest upload (bytes)", Placeholder: "67108864", Help: "The size limit of one uploaded file."},
	}},
	{Title: "Jobs", Intro: "How the jobs are run.", Fields: []settingField{
		{Key: "grace_seconds", Label: "Grace period (seconds)", Placeholder: "30", Help: "How long a server may be unreachable before its waiting jobs go to other servers."},
		{Key: "max_job_attempts", Label: "Attempts per job", Placeholder: "3", Help: "How many times a job is tried again after its server vanished."},
		{Key: "delete_comfy_outputs", Label: "Delete results from ComfyUI", Kind: "select", Options: []string{"true", "false"}, Help: "After a result is saved here, remove it from the output folder of its ComfyUI, through the comfyui-model-crud extension that every server runs."},
		{Key: "preview_soft_limit", Label: "Preview warning (jobs)", Placeholder: "0 (none)", Help: "A preview warns when a command would make more jobs than this."},
	}},
	{Title: "Log and events", Fields: []settingField{
		{Key: "log_level", Label: "Log level", Kind: "select", Options: []string{"debug", "info", "warn", "error"}, Help: "At debug every request and every workflow sent to ComfyUI is logged."},
		{Key: "event_log_size", Label: "Events kept", Placeholder: "100", Help: "How many events are kept for clients that ask what they missed (at least 100)."},
	}},
}

func (u *UI) settingsPage(r *http.Request, saved bool) (settingsPage, error) {
	view, err := u.Services.Settings.Get(r.Context())
	if err != nil {
		return settingsPage{}, err
	}
	page := settingsPage{DBPath: view.DBPath, CanRestart: u.Restart != nil, Saved: saved}
	for _, section := range settingsLayout {
		out := settingSection{Title: section.Title, Intro: section.Intro}
		for _, f := range section.Fields {
			if f.Kind == "" {
				f.Kind = "text"
			}
			f.Value, f.Set = view.Values[f.Key], view.SecretSet[f.Key]
			if view.Overridden[f.Key] {
				f.Override = config.EnvPrefix + strings.ToUpper(f.Key)
			}
			out.Fields = append(out.Fields, f)
		}
		page.Sections = append(page.Sections, out)
	}
	return page, nil
}

func (u *UI) settingsRoutes() {
	u.handle("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		p, err := u.settingsPage(r, false)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		u.renderPage(w, r, "settings", page{Title: "Settings", Nav: "settings", Data: p})
	})
	u.handle("POST /ui/settings", u.actForm("settings-error", func(r *http.Request, f *form, s *stream) error {
		in := service.SettingsInput{Values: map[string]string{}}
		for _, key := range config.Keys() {
			if _, sent := f.values[key]; sent {
				in.Values[key] = f.str(key)
			}
			if config.IsSecret(key) && f.checked("clear_"+key) {
				in.ClearSecrets = append(in.ClearSecrets, key)
			}
		}
		if err := u.Services.Settings.Update(r.Context(), in); err != nil {
			return err
		}
		p, err := u.settingsPage(r, true)
		if err != nil {
			return err
		}
		if err := s.into("settings-error", "error_text", ""); err != nil {
			return err
		}
		if err := s.into("settings-form-body", "settings_fields", p); err != nil {
			return err
		}
		if err := s.render("settings_restart", p); err != nil {
			return err
		}
		return s.toast("ok", "Saved. Restart the program to apply it.")
	}))
	u.handle("POST /ui/settings/restart", act(u, "", func(_ *http.Request, _ *struct{}, s *stream) error {
		if u.Restart == nil {
			return errors.New("InvalidRequest", "this program cannot restart itself; stop it and start it again")
		}
		if err := s.toast("ok", "Restarting… the page comes back by itself."); err != nil {
			return err
		}
		u.Restart()
		return nil
	}))
}
