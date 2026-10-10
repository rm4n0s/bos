// Package config holds the settings of the application (SPEC §2).
//
// The settings live in the database, in a table the settings page edits. This
// package knows their names, defaults and rules, and turns the stored values,
// with the BOS_<SETTING> environment variables laid over them for one run, into
// a Config. It does no I/O.
package config

import (
	"log/slog"
	"net"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// EnvPrefix starts the name of every environment variable that overrides a
// configuration field: BOS_DATABASE, BOS_LISTEN_ADDR, …
const EnvPrefix = "BOS_"

// Defaults for the optional settings of SPEC §2.
const (
	DefaultCivitaiDomain    = "civitai.com"
	DefaultCivitaiPause     = 15
	DefaultLogLevel         = "info"
	DefaultGraceSeconds     = 30
	DefaultMaxJobAttempts   = 3
	DefaultListenAddr       = "127.0.0.1:8080"
	DefaultUploadLimitBytes = 64 << 20
	DefaultEventLogSize     = 100
	// MinEventLogSize is the smallest event log SPEC §8.5 allows.
	MinEventLogSize = 100
)

// Config is the configuration of one run: the settings stored in the database,
// with the environment laid over them, and the defaults for what is not set.
type Config struct {
	// Database is the path of the database file, given on the command line. It
	// is not a setting: the settings are inside it.
	Database        string `json:"-"`
	ResultsDir      string `json:"results_dir"`
	ControlNetDir   string `json:"controlnet_dir"`
	IPAdapterDir    string `json:"ipadapter_dir"`
	RegionsDir      string `json:"regions_dir"`
	ThumbnailsDir   string `json:"thumbnails_dir"`
	DownloadsDir    string `json:"downloads_dir"`
	CivitaiCacheDir string `json:"civitai_cache_dir"`
	CivitaiToken    string `json:"civitai_token"`
	CivitaiDomain   string `json:"civitai_domain"`
	// CivitaiPauseSeconds is the pause between two downloads of one import.
	CivitaiPauseSeconds int      `json:"civitai_pause_seconds"`
	GraceSeconds        int      `json:"grace_seconds"`
	MaxJobAttempts      int      `json:"max_job_attempts"`
	PreviewSoftMax      int      `json:"preview_soft_limit"`
	ListenAddr          string   `json:"listen_addr"`
	AuthToken           string   `json:"auth_token"`
	UploadLimit         int64    `json:"upload_limit_bytes"`
	CORSAllowList       []string `json:"cors_allow_list"`
	EventLogSize        int      `json:"event_log_size"`
	// LogLevel is debug, info, warn or error; at debug every request is logged.
	LogLevel string `json:"log_level"`
}

// settingName is the name of a field as a setting: its JSON name. The fields
// that are not settings have none.
func settingName(f reflect.StructField) string {
	name := strings.Split(f.Tag.Get("json"), ",")[0]
	if name == "-" {
		return ""
	}
	return name
}

// Keys lists the names of the settings, in the order of the settings page.
func Keys() []string {
	t := reflect.TypeFor[Config]()
	var keys []string
	for i := range t.NumField() {
		if name := settingName(t.Field(i)); name != "" {
			keys = append(keys, name)
		}
	}
	return keys
}

// IsSecret says whether a setting is a credential: the settings page does not
// show its value.
func IsSecret(key string) bool { return key == "auth_token" || key == "civitai_token" }

// DirKeys are the settings that name a folder the program writes to. A new
// database gets a default for each (DefaultDirs).
func DirKeys() []string {
	return []string{"results_dir", "controlnet_dir", "ipadapter_dir", "regions_dir", "thumbnails_dir", "downloads_dir", "civitai_cache_dir"}
}

// DefaultDirs are the folders a new database starts with: next to the database
// file, as full paths, so that they do not move with the working directory. A
// database in memory has no folder, so the current one is used.
func DefaultDirs(dbPath string) map[string]string {
	base := "."
	if dbPath != "" && dbPath != ":memory:" && !strings.HasPrefix(dbPath, "file:") {
		base = filepath.Dir(dbPath)
	}
	if abs, err := filepath.Abs(base); err == nil {
		base = abs
	}
	return map[string]string{
		"results_dir":       filepath.Join(base, "results"),
		"controlnet_dir":    filepath.Join(base, "controlnet"),
		"ipadapter_dir":     filepath.Join(base, "ipadapter"),
		"regions_dir":       filepath.Join(base, "regions"),
		"thumbnails_dir":    filepath.Join(base, "thumbnails"),
		"downloads_dir":     filepath.Join(base, "downloads"),
		"civitai_cache_dir": filepath.Join(base, "civitai"),
	}
}

// MissingDefaults are the folder settings that the stored settings do not have
// yet, with their defaults: what a new database, or one from before the
// settings were stored in it, is given on its first start.
func MissingDefaults(stored map[string]string, dbPath string) map[string]string {
	out := map[string]string{}
	for key, dir := range DefaultDirs(dbPath) {
		if stored[key] == "" {
			out[key] = dir
		}
	}
	return out
}

// Resolve builds the configuration of a run from the settings stored in the
// database (a name to a text), the path of the database file and the
// environment, whose BOS_<SETTING> variables win. It also returns the settings
// the environment overrode, by name, so that the settings page can say so.
// Stored names it does not know are ignored; every problem is reported at once.
func Resolve(stored map[string]string, dbPath string, getenv func(string) string) (*Config, []string, error) {
	cfg := &Config{}
	var problems []string
	// The folders that were never set are the ones next to the database.
	defaults := DefaultDirs(dbPath)
	layered := func(name string) string {
		if v := stored[name]; v != "" {
			return v
		}
		return defaults[name]
	}
	problems = append(problems, cfg.applyValues(layered, func(name string) string { return name })...)
	var overridden []string
	if getenv != nil {
		env := func(name string) string { return getenv(EnvPrefix + strings.ToUpper(name)) }
		for _, key := range Keys() {
			if env(key) != "" {
				overridden = append(overridden, key)
			}
		}
		problems = append(problems, cfg.applyValues(env, func(name string) string { return EnvPrefix + strings.ToUpper(name) })...)
	}
	if len(problems) > 0 {
		return nil, nil, errors.New("ConfigInvalid", "configuration is not valid", "problems", problems)
	}
	cfg.Database = dbPath
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	slices.Sort(overridden)
	return cfg, overridden, nil
}

// applyValues sets the fields whose lookup is not empty. label names a setting
// in a problem. It returns the problems, one for each value that does not parse.
func (c *Config) applyValues(lookup, label func(name string) string) []string {
	v := reflect.ValueOf(c).Elem()
	t := v.Type()
	var problems []string
	for i := range t.NumField() {
		name := settingName(t.Field(i))
		if name == "" {
			continue
		}
		raw := lookup(name)
		if raw == "" {
			continue
		}
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString(raw)
		case reflect.Int, reflect.Int64:
			n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
			if err != nil {
				problems = append(problems, label(name)+" must be an integer")
				continue
			}
			f.SetInt(n)
		case reflect.Slice:
			var list []string
			for _, part := range strings.Split(raw, ",") {
				if part = strings.TrimSpace(part); part != "" {
					list = append(list, part)
				}
			}
			f.Set(reflect.ValueOf(list))
		default:
			problems = append(problems, label(name)+" has an unsupported type")
		}
	}
	return problems
}

// Values returns every setting of the configuration as the text that Resolve
// reads back: numbers in digits, a list separated by commas.
func (c *Config) Values() map[string]string {
	v := reflect.ValueOf(c).Elem()
	t := v.Type()
	out := map[string]string{}
	for i := range t.NumField() {
		name := settingName(t.Field(i))
		if name == "" {
			continue
		}
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			out[name] = f.String()
		case reflect.Int, reflect.Int64:
			out[name] = strconv.FormatInt(f.Int(), 10)
		case reflect.Slice:
			out[name] = strings.Join(f.Interface().([]string), ", ")
		}
	}
	return out
}

func (c *Config) applyDefaults() {
	if c.CivitaiDomain == "" {
		c.CivitaiDomain = DefaultCivitaiDomain
	}
	if c.LogLevel == "" {
		c.LogLevel = DefaultLogLevel
	}
	if c.CivitaiPauseSeconds == 0 {
		c.CivitaiPauseSeconds = DefaultCivitaiPause
	}
	if c.GraceSeconds == 0 {
		c.GraceSeconds = DefaultGraceSeconds
	}
	if c.MaxJobAttempts == 0 {
		c.MaxJobAttempts = DefaultMaxJobAttempts
	}
	if c.ListenAddr == "" {
		c.ListenAddr = DefaultListenAddr
	}
	if c.UploadLimit == 0 {
		c.UploadLimit = DefaultUploadLimitBytes
	}
	if c.EventLogSize == 0 {
		c.EventLogSize = DefaultEventLogSize
	}
}

// Validate reports every problem at once in the metadata of one ConfigInvalid
// error, so the user can fix the file in a single pass.
func (c *Config) Validate() error {
	var problems []string
	required := []struct{ name, value string }{
		{"results_dir", c.ResultsDir},
		{"controlnet_dir", c.ControlNetDir},
		{"ipadapter_dir", c.IPAdapterDir},
		{"regions_dir", c.RegionsDir},
		{"thumbnails_dir", c.ThumbnailsDir},
		{"downloads_dir", c.DownloadsDir},
	}
	for _, r := range required {
		if strings.TrimSpace(r.value) == "" {
			problems = append(problems, r.name+" is required")
		}
	}
	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		problems = append(problems, "listen_addr must be host:port")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		problems = append(problems, "log_level must be debug, info, warn or error")
	}
	if c.CivitaiPauseSeconds < 0 {
		problems = append(problems, "civitai_pause_seconds must not be negative")
	}
	if c.GraceSeconds < 0 {
		problems = append(problems, "grace_seconds must not be negative")
	}
	if c.MaxJobAttempts < 1 {
		problems = append(problems, "max_job_attempts must be at least 1")
	}
	if c.PreviewSoftMax < 0 {
		problems = append(problems, "preview_soft_limit must not be negative")
	}
	if c.UploadLimit < 1 {
		problems = append(problems, "upload_limit_bytes must be positive")
	}
	if c.EventLogSize < MinEventLogSize {
		problems = append(problems, "event_log_size must be at least "+strconv.Itoa(MinEventLogSize))
	}
	if len(problems) > 0 {
		return errors.New("ConfigInvalid", "configuration is not valid", "problems", problems)
	}
	return nil
}

// Directories lists every directory that must exist at startup (SPEC §2). The
// optional Civitai cache is included only when configured.
func (c *Config) Directories() []string {
	dirs := []string{c.ResultsDir, c.ControlNetDir, c.IPAdapterDir, c.RegionsDir, c.ThumbnailsDir, c.DownloadsDir}
	if c.CivitaiCacheDir != "" {
		dirs = append(dirs, c.CivitaiCacheDir)
	}
	return dirs
}

// ExposedWithoutAuth reports whether the server listens on a non-loopback
// address without an auth token, which SPEC §16.6 asks to warn about.
func (c *Config) ExposedWithoutAuth() bool {
	if c.AuthToken != "" {
		return false
	}
	host, _, err := net.SplitHostPort(c.ListenAddr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

// Level is the slog level named by LogLevel.
func (c *Config) Level() slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return slog.LevelInfo // Validate has refused anything else
	}
	return l
}
