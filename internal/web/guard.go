package web

import (
	"mime"
	"net/http"
	"strconv"
	"strings"
)

const (
	maxBody       = 1 << 20  // 1 MiB
	maxBackupBody = 10 << 20 // 10 MiB
)

const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// guard wraps every request. The app is only published on 127.0.0.1, but
// any web page the user visits can still aim requests at it, so:
//   - the Host must be a loopback name on our port (blocks DNS rebinding);
//   - state-changing requests need a custom header and a JSON body, which a
//     cross-site page can't send without a CORS preflight we never grant;
//   - request bodies are size-capped.
func guard(port int, next http.Handler) http.Handler {
	p := strconv.Itoa(port)
	allowedHosts := map[string]bool{
		"localhost:" + p: true,
		"127.0.0.1:" + p: true,
		"[::1]:" + p:     true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		} else {
			// Embedded files have no modtime to revalidate against; always
			// refetch so an updated container never runs a stale app.js.
			h.Set("Cache-Control", "no-cache")
		}

		if !allowedHosts[strings.ToLower(r.Host)] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			origin := r.Header.Get("Origin")
			if r.Header.Get("X-PTT-Request") != "1" || mt != "application/json" ||
				(origin != "" && !strings.EqualFold(origin, "http://"+r.Host)) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		limit := int64(maxBody)
		if strings.HasPrefix(r.URL.Path, "/api/backup/") {
			limit = maxBackupBody
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}
