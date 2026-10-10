package service

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/bos/internal/config"
	"github.com/rm4n0s/errors"
)

func settingsEnv(t *testing.T, overridden ...string) *env {
	return newEnv(t, func(d *Deps) { d.DBPath, d.Overridden = "/srv/bos/bos.db", overridden })
}

func TestSettingsStartAtTheirDefaults(t *testing.T) {
	e := settingsEnv(t)
	got, err := e.Settings.Get(ctx(t))
	check(t, err)
	if got.Values["listen_addr"] != config.DefaultListenAddr || got.Values["grace_seconds"] != "30" || got.Values["results_dir"] != "/srv/bos/results" {
		t.Errorf("values = %v", got.Values)
	}
	for _, key := range config.Keys() {
		if _, ok := got.Values[key]; !ok {
			t.Errorf("no value for %s", key)
		}
	}
	if got.DBPath != "/srv/bos/bos.db" {
		t.Errorf("DBPath = %q", got.DBPath)
	}
}

func TestSettingsAreSavedAndRead(t *testing.T) {
	e := settingsEnv(t)
	check(t, e.Settings.Update(ctx(t), SettingsInput{Values: map[string]string{
		"listen_addr": "0.0.0.0:9000", "grace_seconds": "5", "cors_allow_list": "http://a.example, http://b.example", "results_dir": "/data/results",
	}}))
	got, err := e.Settings.Get(ctx(t))
	check(t, err)
	if got.Values["listen_addr"] != "0.0.0.0:9000" || got.Values["grace_seconds"] != "5" || got.Values["results_dir"] != "/data/results" ||
		got.Values["cors_allow_list"] != "http://a.example, http://b.example" {
		t.Errorf("values = %v", got.Values)
	}
	// What is stored is what was given, not the whole configuration.
	stored, err := e.repos.Settings().All(ctx(t))
	check(t, err)
	if len(stored) != 4 {
		t.Errorf("stored = %v", stored)
	}

	// An empty value puts a setting back to its default; a folder too.
	check(t, e.Settings.Update(ctx(t), SettingsInput{Values: map[string]string{"grace_seconds": "", "results_dir": ""}}))
	got, err = e.Settings.Get(ctx(t))
	check(t, err)
	if got.Values["grace_seconds"] != "30" || got.Values["results_dir"] != "/srv/bos/results" || got.Values["listen_addr"] != "0.0.0.0:9000" {
		t.Errorf("after the reset: %v", got.Values)
	}
}

// A credential is never given back; an empty one on save leaves it as it is,
// and it is removed only by asking.
func TestSecretsAreKeptHidden(t *testing.T) {
	e := settingsEnv(t)
	check(t, e.Settings.Update(ctx(t), SettingsInput{Values: map[string]string{"civitai_token": "tok-1", "auth_token": "pass"}}))
	got, err := e.Settings.Get(ctx(t))
	check(t, err)
	if got.Values["civitai_token"] != "" || got.Values["auth_token"] != "" {
		t.Errorf("a secret came back: %v", got.Values)
	}
	if !got.SecretSet["civitai_token"] || !got.SecretSet["auth_token"] {
		t.Errorf("secrets set = %v", got.SecretSet)
	}
	stored, _ := e.repos.Settings().All(ctx(t))
	if stored["civitai_token"] != "tok-1" {
		t.Errorf("stored token = %q", stored["civitai_token"])
	}

	// Saving the other settings with the secrets left empty keeps them.
	check(t, e.Settings.Update(ctx(t), SettingsInput{Values: map[string]string{"civitai_token": "", "grace_seconds": "7"}}))
	stored, _ = e.repos.Settings().All(ctx(t))
	if stored["civitai_token"] != "tok-1" || stored["auth_token"] != "pass" {
		t.Errorf("an empty secret removed the stored one: %v", stored)
	}
	// A new value replaces it.
	check(t, e.Settings.Update(ctx(t), SettingsInput{Values: map[string]string{"civitai_token": "tok-2"}}))
	stored, _ = e.repos.Settings().All(ctx(t))
	if stored["civitai_token"] != "tok-2" {
		t.Errorf("stored token = %q", stored["civitai_token"])
	}
	// Removing is asked for by name.
	check(t, e.Settings.Update(ctx(t), SettingsInput{ClearSecrets: []string{"civitai_token"}}))
	got, _ = e.Settings.Get(ctx(t))
	stored, _ = e.repos.Settings().All(ctx(t))
	if got.SecretSet["civitai_token"] || stored["civitai_token"] != "" || !got.SecretSet["auth_token"] {
		t.Errorf("after the clear: set %v, stored %v", got.SecretSet, stored)
	}
	wantRoute(t, e.Settings.Update(ctx(t), SettingsInput{ClearSecrets: []string{"listen_addr"}}), "SettingsService.Update", "InvalidRequest")
}

func TestSettingsAreChecked(t *testing.T) {
	e := settingsEnv(t)
	check(t, e.Settings.Update(ctx(t), SettingsInput{Values: map[string]string{"grace_seconds": "9"}}))
	err := e.Settings.Update(ctx(t), SettingsInput{Values: map[string]string{"listen_addr": "nope", "log_level": "loud", "grace_seconds": "x"}})
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "ConfigInvalid" || !appErr.HasRoute("SettingsService.Update") {
		t.Fatalf("error = %v", err)
	}
	got, _ := appErr.Metadata["problems"].([]string)
	want := []string{"grace_seconds must be an integer"}
	if !reflect.DeepEqual(got, want) {
		// Values that do not parse are reported first; the rest once those are fixed.
		t.Errorf("problems = %v, want %v", got, want)
	}
	// Nothing was written by the refused update.
	stored, _ := e.repos.Settings().All(ctx(t))
	if !reflect.DeepEqual(stored, map[string]string{"grace_seconds": "9"}) {
		t.Errorf("stored = %v", stored)
	}
	err = e.Settings.Update(ctx(t), SettingsInput{Values: map[string]string{"listen_addr": "nope", "log_level": "loud"}})
	appErr, _ = errors.FromError(err)
	problems, _ := appErr.Metadata["problems"].([]string)
	if len(problems) != 2 {
		t.Errorf("problems = %v", problems)
	}
	wantRoute(t, e.Settings.Update(ctx(t), SettingsInput{Values: map[string]string{"database": "x"}}), "SettingsService.Update", "InvalidRequest")
}

func TestSettingsSayWhatTheEnvironmentOverrides(t *testing.T) {
	e := settingsEnv(t, "listen_addr", "log_level")
	got, err := e.Settings.Get(ctx(t))
	check(t, err)
	if !got.Overridden["listen_addr"] || !got.Overridden["log_level"] || got.Overridden["grace_seconds"] {
		t.Errorf("overridden = %v", got.Overridden)
	}
}
