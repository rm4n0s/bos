package ui

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

// sessionCookie is the name of the signed cookie (SPEC §16.6).
const sessionCookie = "bos_session"

// securityHeaders sets the headers every response carries. Datastar compiles
// the expressions of its attributes with the Function constructor, so the
// script policy needs 'unsafe-eval'; scripts still come only from this origin.
func (u *UI) securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'none'; script-src 'self' 'unsafe-eval'; style-src 'self'; img-src 'self' data:; " +
		"connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// csrf rejects a state-changing request that does not come from this
// interface: it needs the Datastar-Request header, and the browser must say it
// is the same origin (SPEC §16.6). The sign-in form is checked on the origin
// alone, because it is a plain form post.
func (u *UI) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if !sameOrigin(r) || r.URL.Path != "/login" && r.URL.Path != "/logout" && r.Header.Get("Datastar-Request") != "true" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether the browser says the request is from this origin.
// A request without Origin and Fetch-Metadata (a command-line client) is
// allowed here; it still needs the Datastar header and, when a token is set,
// the cookie.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host == r.Host
}

// sessionValue is what the cookie holds: a keyed hash of a fixed label, so the
// token itself never leaves the server and the cookie cannot be guessed.
func (u *UI) sessionValue() string {
	mac := hmac.New(sha256.New, []byte(u.AuthToken))
	mac.Write([]byte("bos-session-v1")) // a hash.Hash never returns an error
	return hex.EncodeToString(mac.Sum(nil))
}

func (u *UI) signedIn(r *http.Request) bool {
	if u.AuthToken == "" {
		return true
	}
	c, err := r.Cookie(sessionCookie)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(u.sessionValue())) == 1
}

// authenticate sends a visitor without the cookie to the sign-in page. The
// static files and the sign-in page itself stay open.
func (u *UI) authenticate(next http.Handler) http.Handler {
	if u.AuthToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/static/") || u.signedIn(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/ui/") {
			http.Error(w, "sign in first", http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

func (u *UI) authRoutes() {
	u.mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		if u.signedIn(r) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		u.renderPage(w, r, "login", page{Title: "Sign in"})
	})
	u.mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		got := []byte(r.PostForm.Get("token"))
		if u.AuthToken == "" || subtle.ConstantTimeCompare(got, []byte(u.AuthToken)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			u.renderPage(w, r, "login", page{Title: "Sign in", Error: "That token is not right."})
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: sessionCookie, Value: u.sessionValue(), Path: "/",
			HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	u.mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}
