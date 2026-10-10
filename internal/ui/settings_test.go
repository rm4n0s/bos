package ui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/config"
	"github.com/rm4n0s/bos/internal/service"
)

func TestSettingsPage(t *testing.T) {
	u := newTestUI(t)
	check(t, u.env.Services.Settings.Update(t.Context(), service.SettingsInput{Values: map[string]string{
		"listen_addr": "0.0.0.0:9000", "civitai_token": "tok-secret", "grace_seconds": "7", "cors_allow_list": "http://a.example",
	}}))
	page := u.get("/settings")
	if page.Status != 200 {
		t.Fatalf("status = %d", page.Status)
	}
	// Every setting has its field, under its own name.
	for _, key := range config.Keys() {
		mustContain(t, "field "+key, page.Body, `name="`+key+`"`)
	}
	mustContain(t, "values", page.Body, `value="0.0.0.0:9000"`, `value="7"`, `value="http://a.example"`, "bos.db", `aria-current="page">Settings`)
	// A credential is never written into the page: it says whether it is set.
	mustNotContain(t, "secret", page.Body, "tok-secret")
	mustContain(t, "secret", page.Body, `type="password" name="civitai_token"`, "a value is set", `name="clear_civitai_token"`)
	mustContain(t, "secret not set", page.Body, `type="password" name="auth_token"`, "no value")
	// The way back to the defaults and to the folders is on the page.
	mustContain(t, "help", page.Body, "empty", "default")
	if r := u.get("/settings"); !strings.Contains(r.Header.Get("Content-Type"), "text/html") {
		t.Errorf("content type = %q", r.Header.Get("Content-Type"))
	}
}

func TestSettingsAreSaved(t *testing.T) {
	u := newTestUI(t)
	form := func(extra ...part) []part {
		parts := []part{field("listen_addr", "127.0.0.1:8081"), field("grace_seconds", "11"), field("log_level", "debug")}
		return append(parts, extra...)
	}
	r := u.submit("POST", "/ui/settings", form(field("civitai_token", "tok-1"))...)
	mustContain(t, "saved", r.patches(), "Saved.", "restart", `id="settings-restart"`)
	got, err := u.env.Services.Settings.Get(t.Context())
	check(t, err)
	if got.Values["listen_addr"] != "127.0.0.1:8081" || got.Values["grace_seconds"] != "11" || got.Values["log_level"] != "debug" || !got.SecretSet["civitai_token"] {
		t.Errorf("settings = %v, %v", got.Values, got.SecretSet)
	}

	// An empty credential leaves the stored one; a ticked box removes it.
	u.submit("POST", "/ui/settings", form(field("civitai_token", ""))...)
	got, _ = u.env.Services.Settings.Get(t.Context())
	if !got.SecretSet["civitai_token"] {
		t.Error("an empty field removed the token")
	}
	u.submit("POST", "/ui/settings", form(field("clear_civitai_token", "on"))...)
	got, _ = u.env.Services.Settings.Get(t.Context())
	if got.SecretSet["civitai_token"] {
		t.Error("the token was not removed")
	}
	// An empty value is the default again.
	u.submit("POST", "/ui/settings", field("grace_seconds", ""))
	got, _ = u.env.Services.Settings.Get(t.Context())
	if got.Values["grace_seconds"] != "30" {
		t.Errorf("grace_seconds = %q", got.Values["grace_seconds"])
	}
}

func TestSettingsThatAreWrongAreRefused(t *testing.T) {
	u := newTestUI(t)
	r := u.submit("POST", "/ui/settings", field("listen_addr", "nope"), field("log_level", "loud"))
	mustContain(t, "refused", r.patches(), "selector #settings-error", "listen_addr must be host:port", "log_level must be debug, info, warn or error")
	got, _ := u.env.Services.Settings.Get(t.Context())
	if got.Values["listen_addr"] != config.DefaultListenAddr {
		t.Errorf("a refused save changed the settings: %v", got.Values["listen_addr"])
	}
	r = u.submit("POST", "/ui/settings", field("grace_seconds", "soon"))
	mustContain(t, "not a number", r.patches(), "selector #settings-error", "grace_seconds must be an integer")
}

func TestSettingsSayWhatTheEnvironmentOverrides(t *testing.T) {
	u := newTestUIWith(t, []func(*service.Deps){func(d *service.Deps) { d.Overridden = []string{"listen_addr"} }})
	page := u.get("/settings")
	mustContain(t, "override", page.Body, "BOS_LISTEN_ADDR")
	mustNotContain(t, "override", page.Body, "BOS_GRACE_SECONDS")
}

func TestRestartFromTheSettingsPage(t *testing.T) {
	restarts := 0
	u := newTestUI(t, func(d *Deps) { d.Restart = func() { restarts++ } })
	mustContain(t, "button", u.get("/settings").Body, "/ui/settings/restart")
	r := u.datastar("POST", "/ui/settings/restart", struct{}{})
	mustContain(t, "restart", r.patches(), "Restarting")
	if restarts != 1 {
		t.Errorf("restarts = %d", restarts)
	}
	// A forged request is refused like every state change.
	if r := u.send("POST", "/ui/settings/restart", nil, nil); r.Status != http.StatusForbidden {
		t.Errorf("without the header: status = %d", r.Status)
	}
	if restarts != 1 {
		t.Errorf("restarts = %d after a forged request", restarts)
	}

	// Without a way to restart the page does not offer one.
	u2 := newTestUI(t)
	mustNotContain(t, "no restart", u2.get("/settings").Body, "/ui/settings/restart")
	mustContain(t, "refused", u2.datastar("POST", "/ui/settings/restart", struct{}{}).patches(), `class="toast error"`)
}
