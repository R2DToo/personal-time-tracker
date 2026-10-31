package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testPort = 24871

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if _, err := io.ReadAll(r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	w.WriteHeader(http.StatusOK)
})

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func mutation(method, path, host, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = host
	r.Header.Set("X-PTT-Request", "1")
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestGuardHost(t *testing.T) {
	h := guard(testPort, okHandler)
	cases := map[string]int{
		"localhost:24871":   200,
		"LOCALHOST:24871":   200,
		"127.0.0.1:24871":   200,
		"[::1]:24871":       200,
		"evil.com":          403,
		"localhost:8080":    403,
		"192.168.1.5:24871": 403,
		"localhost":         403,
		"":                  403,
	}
	for host, want := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = host
		if got := do(h, r).Code; got != want {
			t.Errorf("host %q: %d, want %d", host, got, want)
		}
	}
}

func TestGuardMutations(t *testing.T) {
	h := guard(testPort, okHandler)
	const host = "localhost:24871"
	if c := do(h, mutation("POST", "/api/projects", host, "{}")).Code; c != 200 {
		t.Errorf("good POST: %d", c)
	}
	r := mutation("POST", "/api/projects", host, "{}")
	r.Header.Del("X-PTT-Request")
	if c := do(h, r).Code; c != 403 {
		t.Errorf("missing header: %d", c)
	}
	r = mutation("PUT", "/api/entries", host, "{}")
	r.Header.Set("Content-Type", "text/plain")
	if c := do(h, r).Code; c != 403 {
		t.Errorf("text/plain: %d", c)
	}
	r = mutation("POST", "/api/projects", host, "{}")
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	if c := do(h, r).Code; c != 200 {
		t.Errorf("json with charset: %d", c)
	}
	r = mutation("POST", "/api/projects", host, "{}")
	r.Header.Set("Origin", "http://evil.com")
	if c := do(h, r).Code; c != 403 {
		t.Errorf("evil origin: %d", c)
	}
	r = mutation("POST", "/api/projects", host, "{}")
	r.Header.Set("Origin", "http://localhost:24871")
	if c := do(h, r).Code; c != 200 {
		t.Errorf("same origin: %d", c)
	}
	g := httptest.NewRequest("GET", "/api/projects", nil)
	g.Host = host
	if c := do(h, g).Code; c != 200 {
		t.Errorf("plain GET: %d", c)
	}
}

func TestGuardHeaders(t *testing.T) {
	h := guard(testPort, okHandler)
	for _, host := range []string{"localhost:24871", "evil.com"} {
		r := httptest.NewRequest("GET", "/api/projects", nil)
		r.Host = host
		rec := do(h, r)
		if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'self'") {
			t.Errorf("%s: CSP = %q", host, rec.Header().Get("Content-Security-Policy"))
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: missing nosniff", host)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: api Cache-Control = %q", host, rec.Header().Get("Cache-Control"))
		}
	}
	// Embedded files carry no modtime, so without this a browser can keep
	// running an old app.js after the consultant updates the container.
	r := httptest.NewRequest("GET", "/app.js", nil)
	r.Host = "localhost:24871"
	if cc := do(h, r).Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("static Cache-Control = %q, want no-cache", cc)
	}
}

func TestGuardBodyLimit(t *testing.T) {
	h := guard(testPort, okHandler)
	big := strings.Repeat("x", 2<<20)
	if c := do(h, mutation("POST", "/api/projects", "localhost:24871", big)).Code; c != http.StatusRequestEntityTooLarge {
		t.Errorf("2 MiB to /api/projects: %d, want 413", c)
	}
	if c := do(h, mutation("POST", "/api/backup/restore", "localhost:24871", big)).Code; c != 200 {
		t.Errorf("2 MiB to /api/backup/restore: %d, want 200", c)
	}
}

// Browsers label requests with Sec-Fetch-Site. Another site (or another
// localhost port, which is "same-site") must not be able to hit the API,
// e.g. <img src=".../api/backup/download"> clearing the backup reminder.
func TestGuardRejectsCrossSiteAPI(t *testing.T) {
	h := guard(testPort, okHandler)
	cases := []struct {
		path, site string
		want       int
	}{
		{"/api/backup/download", "cross-site", 403},
		{"/api/backup/download", "same-site", 403},
		{"/api/backup/download", "same-origin", 200},
		{"/api/backup/download", "none", 200},
		{"/api/backup/download", "", 200}, // non-browser clients (healthcheck, curl)
		{"/", "cross-site", 200},          // following a link to the app is fine
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", c.path, nil)
		r.Host = "localhost:24871"
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		if got := do(h, r).Code; got != c.want {
			t.Errorf("%s with Sec-Fetch-Site %q: %d, want %d", c.path, c.site, got, c.want)
		}
	}
}
