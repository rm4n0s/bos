package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
)

const minimalJSON = `{
  "database": "bos.db",
  "results_dir": "data/results",
  "controlnet_dir": "data/controlnet",
  "ipadapter_dir": "data/ipadapter",
  "regions_dir": "data/regions",
  "thumbnails_dir": "data/thumbnails",
  "downloads_dir": "data/downloads"
}`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bos.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimalJSON), envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"CivitaiDomain":       DefaultCivitaiDomain,
		"GraceSeconds":        DefaultGraceSeconds,
		"CivitaiPauseSeconds": DefaultCivitaiPause,
		"MaxJobAttempts":      DefaultMaxJobAttempts,
		"ListenAddr":          DefaultListenAddr,
		"UploadLimit":         int64(DefaultUploadLimitBytes),
		"EventLogSize":        DefaultEventLogSize,
		"AuthToken":           "",
	}
	v := reflect.ValueOf(cfg).Elem()
	for field, w := range want {
		if got := v.FieldByName(field).Interface(); got != w {
			t.Errorf("%s = %v, want %v", field, got, w)
		}
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	env := envFrom(map[string]string{
		"BOS_DATABASE":           ":memory:",
		"BOS_LISTEN_ADDR":        "0.0.0.0:9000",
		"BOS_GRACE_SECONDS":      "5",
		"BOS_UPLOAD_LIMIT_BYTES": "1024",
		"BOS_CORS_ALLOW_LIST":    "http://a.example, http://b.example,",
		"BOS_AUTH_TOKEN":         "secret",
	})
	cfg, err := Load(writeConfig(t, minimalJSON), env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database != ":memory:" || cfg.ListenAddr != "0.0.0.0:9000" || cfg.GraceSeconds != 5 ||
		cfg.UploadLimit != 1024 || cfg.AuthToken != "secret" {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if want := []string{"http://a.example", "http://b.example"}; !reflect.DeepEqual(cfg.CORSAllowList, want) {
		t.Errorf("CORSAllowList = %v, want %v", cfg.CORSAllowList, want)
	}
}

func TestLoadFromEnvironmentAlone(t *testing.T) {
	env := envFrom(map[string]string{
		"BOS_DATABASE": ":memory:", "BOS_RESULTS_DIR": "r", "BOS_CONTROLNET_DIR": "c",
		"BOS_IPADAPTER_DIR": "i", "BOS_REGIONS_DIR": "g", "BOS_THUMBNAILS_DIR": "t", "BOS_DOWNLOADS_DIR": "d",
	})
	if _, err := Load("", env); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFailures(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		missing  bool
		env      map[string]string
		route    string
		problems []string
	}{
		{
			name:    "file missing",
			missing: true,
			route:   "Load.ConfigNotReadable",
		},
		{
			name:     "malformed JSON",
			content:  `{"database":`,
			route:    "Load.ConfigInvalid",
			problems: []string{"unexpected EOF"},
		},
		{
			name:     "unknown field",
			content:  `{"databse":"x"}`,
			route:    "Load.ConfigInvalid",
			problems: []string{`json: unknown field "databse"`},
		},
		{
			name:     "non-numeric environment value",
			content:  minimalJSON,
			env:      map[string]string{"BOS_GRACE_SECONDS": "soon"},
			route:    "Load->Config.applyEnv.ConfigInvalid",
			problems: []string{"BOS_GRACE_SECONDS must be an integer"},
		},
		{
			name:    "every required field missing",
			content: `{}`,
			route:   "Load->Config.Validate.ConfigInvalid",
			problems: []string{
				"database is required", "results_dir is required", "controlnet_dir is required",
				"ipadapter_dir is required", "regions_dir is required", "thumbnails_dir is required",
				"downloads_dir is required",
			},
		},
		{
			name:     "bad values",
			content:  `{"database":"d","results_dir":"a","controlnet_dir":"b","ipadapter_dir":"c","regions_dir":"e","thumbnails_dir":"f","downloads_dir":"g","listen_addr":"nope","grace_seconds":-1,"civitai_pause_seconds":-3,"max_job_attempts":-2,"event_log_size":5}`,
			route:    "Load->Config.Validate.ConfigInvalid",
			problems: []string{"listen_addr must be host:port", "civitai_pause_seconds must not be negative", "grace_seconds must not be negative", "max_job_attempts must be at least 1", "event_log_size must be at least 100"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing.json")
			if !tt.missing {
				path = writeConfig(t, tt.content)
			}
			_, err := Load(path, envFrom(tt.env))
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if tt.problems == nil {
				return
			}
			got, _ := appErr.Metadata["problems"].([]string)
			if len(got) != len(tt.problems) {
				t.Fatalf("problems = %v, want %v", got, tt.problems)
			}
			for i := range got {
				if i < len(tt.problems) && got[i] != tt.problems[i] {
					t.Errorf("problem %d = %q, want %q", i, got[i], tt.problems[i])
				}
			}
		})
	}
}

func TestDirectories(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimalJSON), envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(cfg.Directories()); n != 6 {
		t.Errorf("got %d directories, want 6", n)
	}
	cfg.CivitaiCacheDir = "cache"
	dirs := cfg.Directories()
	if len(dirs) != 7 || dirs[6] != "cache" {
		t.Errorf("Civitai cache not listed: %v", dirs)
	}
}

func TestExposedWithoutAuth(t *testing.T) {
	tests := []struct {
		addr, token string
		want        bool
	}{
		{"127.0.0.1:8080", "", false},
		{"[::1]:8080", "", false},
		{"localhost:8080", "", false},
		{"0.0.0.0:8080", "", true},
		{":8080", "", true},
		{"192.168.1.5:8080", "", true},
		{"0.0.0.0:8080", "tok", false},
	}
	for _, tt := range tests {
		c := &Config{ListenAddr: tt.addr, AuthToken: tt.token}
		if got := c.ExposedWithoutAuth(); got != tt.want {
			t.Errorf("ExposedWithoutAuth(%q, %q) = %v, want %v", tt.addr, tt.token, got, tt.want)
		}
	}
}
