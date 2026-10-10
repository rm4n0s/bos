package config

import (
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// dirs gives every folder setting a value, as the first start of a database does.
func dirs() map[string]string { return DefaultDirs(filepath.Join("/srv", "bos", "bos.db")) }

func TestDefaultDirsAreNextToTheDatabase(t *testing.T) {
	got := DefaultDirs("/srv/bos/data/bos.db")
	want := map[string]string{
		"results_dir":       "/srv/bos/data/results",
		"controlnet_dir":    "/srv/bos/data/controlnet",
		"ipadapter_dir":     "/srv/bos/data/ipadapter",
		"regions_dir":       "/srv/bos/data/regions",
		"thumbnails_dir":    "/srv/bos/data/thumbnails",
		"downloads_dir":     "/srv/bos/data/downloads",
		"civitai_cache_dir": "/srv/bos/data/civitai",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultDirs = %v\nwant         %v", got, want)
	}
	// A relative path is made absolute, so the folders do not move with the working directory.
	t.Chdir(t.TempDir())
	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	rel := DefaultDirs("data/bos.db")
	if rel["results_dir"] != filepath.Join(wd, "data", "results") {
		t.Errorf("results_dir = %q", rel["results_dir"])
	}
	// A database in memory has no folder: the current one is used.
	if mem := DefaultDirs(":memory:"); mem["results_dir"] != filepath.Join(wd, "results") {
		t.Errorf("in memory: results_dir = %q", mem["results_dir"])
	}
	// Every folder setting has a default, and the list of keys agrees.
	if len(want) != len(DirKeys()) {
		t.Errorf("DirKeys = %v", DirKeys())
	}
	for _, k := range DirKeys() {
		if _, ok := want[k]; !ok {
			t.Errorf("%s has no default", k)
		}
	}
}

func TestResolveDefaults(t *testing.T) {
	cfg, over, err := Resolve(dirs(), "/srv/bos/bos.db", envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(over) != 0 {
		t.Errorf("overridden = %v", over)
	}
	want := map[string]any{
		"Database":            "/srv/bos/bos.db",
		"CivitaiDomain":       DefaultCivitaiDomain,
		"GraceSeconds":        DefaultGraceSeconds,
		"CivitaiPauseSeconds": DefaultCivitaiPause,
		"LogLevel":            DefaultLogLevel,
		"MaxJobAttempts":      DefaultMaxJobAttempts,
		"ListenAddr":          DefaultListenAddr,
		"UploadLimit":         int64(DefaultUploadLimitBytes),
		"EventLogSize":        DefaultEventLogSize,
		"AuthToken":           "",
		"CivitaiToken":        "",
	}
	v := reflect.ValueOf(cfg).Elem()
	for field, w := range want {
		if got := v.FieldByName(field).Interface(); got != w {
			t.Errorf("%s = %v, want %v", field, got, w)
		}
	}
}

// A folder that is not stored is the default one, so a database never lacks folders.
func TestFoldersDefaultToThoseNextToTheDatabase(t *testing.T) {
	cfg, _, err := Resolve(map[string]string{"results_dir": "/elsewhere/results"}, "/srv/bos/bos.db", envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ResultsDir != "/elsewhere/results" || cfg.ControlNetDir != "/srv/bos/controlnet" || cfg.CivitaiCacheDir != "/srv/bos/civitai" {
		t.Errorf("folders = %q %q %q", cfg.ResultsDir, cfg.ControlNetDir, cfg.CivitaiCacheDir)
	}
}

func TestMissingDefaults(t *testing.T) {
	got := MissingDefaults(map[string]string{"results_dir": "/mine", "listen_addr": "0.0.0.0:1"}, "/srv/bos/bos.db")
	if _, has := got["results_dir"]; has || len(got) != len(DirKeys())-1 || got["civitai_cache_dir"] != "/srv/bos/civitai" {
		t.Errorf("missing = %v", got)
	}
	if len(MissingDefaults(DefaultDirs("/srv/bos/bos.db"), "/srv/bos/bos.db")) != 0 {
		t.Error("nothing is missing from a database that has them")
	}
}

func TestResolveStoredValues(t *testing.T) {
	stored := dirs()
	stored["listen_addr"] = "0.0.0.0:9000"
	stored["grace_seconds"] = "5"
	stored["upload_limit_bytes"] = "1024"
	stored["cors_allow_list"] = "http://a.example, http://b.example,"
	stored["auth_token"] = "secret"
	stored["something_from_the_future"] = "ignored"
	cfg, _, err := Resolve(stored, "/srv/bos/bos.db", envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "0.0.0.0:9000" || cfg.GraceSeconds != 5 || cfg.UploadLimit != 1024 || cfg.AuthToken != "secret" {
		t.Errorf("values not applied: %+v", cfg)
	}
	if want := []string{"http://a.example", "http://b.example"}; !reflect.DeepEqual(cfg.CORSAllowList, want) {
		t.Errorf("CORSAllowList = %v, want %v", cfg.CORSAllowList, want)
	}
}

// The environment wins over the database, for one run, and says what it won over.
func TestEnvironmentOverridesTheStoredValues(t *testing.T) {
	stored := dirs()
	stored["listen_addr"] = "127.0.0.1:7000"
	env := envFrom(map[string]string{"BOS_LISTEN_ADDR": "127.0.0.1:0", "BOS_RESULTS_DIR": "elsewhere", "BOS_LOG_LEVEL": "debug", "BOS_DATABASE": "ignored"})
	cfg, over, err := Resolve(stored, "/srv/bos/bos.db", env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:0" || cfg.ResultsDir != "elsewhere" || cfg.LogLevel != "debug" || cfg.Database != "/srv/bos/bos.db" {
		t.Errorf("overrides: %+v", cfg)
	}
	if want := []string{"listen_addr", "log_level", "results_dir"}; !reflect.DeepEqual(over, want) {
		t.Errorf("overridden = %v, want %v", over, want)
	}
}

func TestResolveFailures(t *testing.T) {
	with := func(extra map[string]string) map[string]string {
		m := dirs()
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	tests := []struct {
		name     string
		stored   map[string]string
		env      map[string]string
		route    string
		problems []string
	}{
		{
			name:     "a stored value that is not a number",
			stored:   with(map[string]string{"grace_seconds": "soon"}),
			route:    "Resolve.ConfigInvalid",
			problems: []string{"grace_seconds must be an integer"},
		},
		{
			name:     "an environment value that is not a number",
			stored:   dirs(),
			env:      map[string]string{"BOS_GRACE_SECONDS": "soon"},
			route:    "Resolve.ConfigInvalid",
			problems: []string{"BOS_GRACE_SECONDS must be an integer"},
		},
		{
			name:     "bad values",
			stored:   with(map[string]string{"listen_addr": "nope", "grace_seconds": "-1", "civitai_pause_seconds": "-3", "log_level": "loud", "max_job_attempts": "-2", "event_log_size": "5"}),
			route:    "Resolve->Config.Validate.ConfigInvalid",
			problems: []string{"listen_addr must be host:port", "log_level must be debug, info, warn or error", "civitai_pause_seconds must not be negative", "grace_seconds must not be negative", "max_job_attempts must be at least 1", "event_log_size must be at least 100"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Resolve(tt.stored, "/srv/bos/bos.db", envFrom(tt.env))
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			got, _ := appErr.Metadata["problems"].([]string)
			if !reflect.DeepEqual(got, tt.problems) {
				t.Errorf("problems = %v, want %v", got, tt.problems)
			}
		})
	}
}

// What a configuration shows in the settings page goes back through Resolve
// unchanged, for every setting and its text.
func TestValuesRoundTrip(t *testing.T) {
	stored := dirs()
	stored["listen_addr"] = "0.0.0.0:9000"
	stored["cors_allow_list"] = "http://a.example, http://b.example"
	stored["civitai_token"] = "tok"
	stored["preview_soft_limit"] = "500"
	cfg, _, err := Resolve(stored, "/srv/bos/bos.db", envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	values := cfg.Values()
	for _, key := range Keys() {
		if _, ok := values[key]; !ok {
			t.Errorf("Values has no %s", key)
		}
	}
	if _, has := values["database"]; has {
		t.Error("the database path is not a setting")
	}
	back, _, err := Resolve(values, "/srv/bos/bos.db", envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, back) {
		t.Errorf("round trip:\n%+v\n%+v", cfg, back)
	}
	if values["upload_limit_bytes"] != "67108864" || values["cors_allow_list"] != "http://a.example, http://b.example" {
		t.Errorf("values = %v", values)
	}
}

func TestSecrets(t *testing.T) {
	for key, want := range map[string]bool{"auth_token": true, "civitai_token": true, "listen_addr": false, "results_dir": false} {
		if IsSecret(key) != want {
			t.Errorf("IsSecret(%q) = %v", key, !want)
		}
	}
	if len(Keys()) < 15 || Keys()[0] != "results_dir" {
		t.Errorf("Keys = %v", Keys())
	}
	for _, k := range Keys() {
		if k != strings.ToLower(k) || strings.Contains(k, " ") {
			t.Errorf("key %q", k)
		}
	}
}

func TestDirectories(t *testing.T) {
	cfg, _, err := Resolve(dirs(), "/srv/bos/bos.db", envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	// The Civitai cache is among the folders: it has a default of its own.
	if n := len(cfg.Directories()); n != 7 {
		t.Errorf("got %d directories, want 7", n)
	}
	cfg.CivitaiCacheDir = ""
	if n := len(cfg.Directories()); n != 6 {
		t.Errorf("without the cache: %d directories, want 6", n)
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

func TestLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError, "": slog.LevelInfo} {
		if got := (&Config{LogLevel: in}).Level(); got != want {
			t.Errorf("Level(%q) = %v, want %v", in, got, want)
		}
	}
}
