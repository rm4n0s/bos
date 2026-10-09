package ui

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func (u *testUI) login(token string) reply {
	u.t.Helper()
	form := url.Values{"token": {token}}.Encode()
	return u.send("POST", "/login", strings.NewReader(form), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}

func TestSignIn(t *testing.T) {
	u := newTestUI(t, func(d *Deps) { d.AuthToken = "s3cret" })

	// Without the cookie a page sends the visitor to the sign-in form and an
	// action is refused; the static files stay open.
	r := u.get("/servers")
	if r.Status != http.StatusSeeOther || r.Header.Get("Location") != "/login" {
		t.Errorf("page: status = %d, Location = %q", r.Status, r.Header.Get("Location"))
	}
	if r := u.datastar("GET", "/ui/servers/table", struct{}{}); r.Status != http.StatusUnauthorized {
		t.Errorf("fragment: status = %d", r.Status)
	}
	if r := u.datastar("POST", "/ui/servers", serverForm(0, "x", "http://x:1", "x")); r.Status != http.StatusUnauthorized {
		t.Errorf("action: status = %d", r.Status)
	}
	if r := u.get("/static/app.css"); r.Status != 200 {
		t.Errorf("static: status = %d", r.Status)
	}
	if r := u.get("/files/results/x.png"); r.Status != http.StatusSeeOther {
		t.Errorf("files: status = %d", r.Status)
	}
	mustContain(t, "sign-in page", u.get("/login").Body, `name="token"`, `type="password"`)

	// A wrong token.
	bad := u.login("nope")
	if bad.Status != http.StatusUnauthorized || bad.Header.Get("Set-Cookie") != "" {
		t.Errorf("bad token: status = %d, cookie = %q", bad.Status, bad.Header.Get("Set-Cookie"))
	}
	mustContain(t, "bad login", bad.Body, "That token is not right.")

	// The right one sets a signed, HttpOnly, SameSite=Strict cookie.
	good := u.login("s3cret")
	if good.Status != http.StatusSeeOther || good.Header.Get("Location") != "/" {
		t.Fatalf("login: status = %d, Location = %q", good.Status, good.Header.Get("Location"))
	}
	cookie := good.Header.Get("Set-Cookie")
	mustContain(t, "cookie", cookie, sessionCookie+"=", "HttpOnly", "SameSite=Strict", "Path=/")
	mustNotContain(t, "cookie", cookie, "s3cret")
	value := strings.TrimPrefix(strings.SplitN(cookie, ";", 2)[0], sessionCookie+"=")

	withCookie := map[string]string{"Cookie": sessionCookie + "=" + value}
	if r := u.send("GET", "/servers", nil, withCookie); r.Status != 200 {
		t.Errorf("signed in: status = %d", r.Status)
	} else {
		mustContain(t, "page", r.Body, "Sign out")
	}
	withCookie["Datastar-Request"] = "true"
	if r := u.send("GET", "/ui/servers/table", nil, withCookie); r.Status != 200 {
		t.Errorf("signed-in fragment: status = %d", r.Status)
	}
	// A forged cookie does not work.
	if r := u.send("GET", "/servers", nil, map[string]string{"Cookie": sessionCookie + "=" + strings.Repeat("a", len(value))}); r.Status != http.StatusSeeOther {
		t.Errorf("forged cookie: status = %d", r.Status)
	}
	// Signing in again while signed in goes home; signing out clears the cookie.
	if r := u.send("GET", "/login", nil, map[string]string{"Cookie": sessionCookie + "=" + value}); r.Status != http.StatusSeeOther {
		t.Errorf("login while signed in: status = %d", r.Status)
	}
	out := u.send("POST", "/logout", nil, map[string]string{"Cookie": sessionCookie + "=" + value})
	mustContain(t, "logout cookie", out.Header.Get("Set-Cookie"), "Max-Age=0")
}

func TestLoginRefusesOtherOrigins(t *testing.T) {
	u := newTestUI(t, func(d *Deps) { d.AuthToken = "s3cret" })
	form := url.Values{"token": {"s3cret"}}.Encode()
	r := u.send("POST", "/login", strings.NewReader(form), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded", "Origin": "https://evil.example",
	})
	if r.Status != http.StatusForbidden {
		t.Errorf("status = %d", r.Status)
	}
}

func TestNoSignInWithoutAToken(t *testing.T) {
	u := newTestUI(t)
	if r := u.get("/login"); r.Status != http.StatusSeeOther {
		t.Errorf("/login: status = %d", r.Status)
	}
	if r := u.login("anything"); r.Status != http.StatusUnauthorized {
		t.Errorf("login without a configured token: status = %d", r.Status)
	}
	mustNotContain(t, "page", u.get("/servers").Body, "Sign out")
}
