// Package config loads and validates the application configuration (SPEC §2).
//
// The configuration is a JSON file whose fields can be overridden by
// environment variables named BOS_<FIELD> (SPEC §14.4, D2).
package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"reflect"
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

// Config is the application configuration. Zero values of optional fields are
// replaced by their defaults when the configuration is loaded.
type Config struct {
	Database        string `json:"database"`
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

// Load reads the JSON file at path, applies environment overrides taken from
// getenv, fills in defaults and validates the result. An empty path means
// "no file": the configuration then comes from the environment alone.
func Load(path string, getenv func(string) string) (*Config, error) {
	cfg := &Config{}
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.NewErr("ConfigNotReadable", err).SetMetadata("path", path)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(cfg); err != nil {
			return nil, errors.NewErr("ConfigInvalid", err).SetMetadata("path", path, "problems", []string{err.Error()})
		}
	}
	if err := cfg.applyEnv(getenv); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyEnv overrides fields from BOS_<JSON NAME> variables. Only variables
// that are set and non-empty take effect.
func (c *Config) applyEnv(getenv func(string) string) error {
	if getenv == nil {
		return nil
	}
	v := reflect.ValueOf(c).Elem()
	t := v.Type()
	var problems []string
	for i := range t.NumField() {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		env := EnvPrefix + strings.ToUpper(name)
		raw := getenv(env)
		if raw == "" {
			continue
		}
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString(raw)
		case reflect.Int, reflect.Int64:
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				problems = append(problems, env+" must be an integer")
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
			problems = append(problems, env+" has an unsupported type")
		}
	}
	if len(problems) > 0 {
		return errors.New("ConfigInvalid", "configuration is not valid", "problems", problems)
	}
	return nil
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
		{"database", c.Database},
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
