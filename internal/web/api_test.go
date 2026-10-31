package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"personaltimetracker/internal/store"
)

type apiClient struct {
	t   *testing.T
	h   http.Handler
	dir string
}

func newAPI(t *testing.T) *apiClient {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir, func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	return &apiClient{t: t, h: NewHandler(st, testPort), dir: dir}
}

func (c *apiClient) call(method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if method == "GET" {
		r = httptest.NewRequest(method, path, nil)
		r.Host = "localhost:24871"
	} else {
		r = mutation(method, path, "localhost:24871", body)
	}
	return do(c.h, r)
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestAPIProjectsFlow(t *testing.T) {
	c := newAPI(t)
	rec := c.call("POST", "/api/projects", `{"name":"Acme"}`)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	p := decode[store.Project](t, rec)
	if rec := c.call("POST", "/api/projects", `{"name":"acme"}`); rec.Code != 409 {
		t.Errorf("duplicate: %d", rec.Code)
	} else if !strings.Contains(decode[map[string]string](t, rec)["error"], "already exists") {
		t.Errorf("duplicate message: %s", rec.Body)
	}
	rec = c.call("PATCH", "/api/projects/"+p.ID, `{"archived":true}`)
	if rec.Code != 200 || !decode[store.Project](t, rec).Archived {
		t.Errorf("archive: %d %s", rec.Code, rec.Body)
	}
	rec = c.call("PATCH", "/api/projects/"+p.ID, `{"name":"Acme Corp"}`)
	if rec.Code != 200 || decode[store.Project](t, rec).Name != "Acme Corp" {
		t.Errorf("rename: %d %s", rec.Code, rec.Body)
	}
	if rec := c.call("PATCH", "/api/projects/p_nope", `{"archived":true}`); rec.Code != 404 {
		t.Errorf("unknown: %d", rec.Code)
	}
	if rec := c.call("PATCH", "/api/projects/"+p.ID, `{"bogus":1}`); rec.Code != 400 {
		t.Errorf("unknown field: %d", rec.Code)
	}
	if rec := c.call("PATCH", "/api/projects/"+p.ID, `{}`); rec.Code != 400 {
		t.Errorf("empty patch: %d", rec.Code)
	}
	list := decode[[]store.Project](t, c.call("GET", "/api/projects", ""))
	if len(list) != 1 {
		t.Errorf("list = %+v", list)
	}
}

func TestAPIEntriesWeekMonth(t *testing.T) {
	c := newAPI(t)
	p := decode[store.Project](t, c.call("POST", "/api/projects", `{"name":"Acme"}`))
	if rec := c.call("PUT", "/api/entries", `{"projectId":"`+p.ID+`","date":"2026-10-26","hours":1.5}`); rec.Code != 204 {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	rec := c.call("GET", "/api/week?start=2026-10-26", "")
	if !strings.Contains(rec.Body.String(), `"hours":[1.5,0,0,0,0,0,0]`) {
		t.Errorf("week = %s", rec.Body)
	}
	rec = c.call("GET", "/api/month?month=2026-10", "")
	if !strings.Contains(rec.Body.String(), `"total":1.5`) {
		t.Errorf("month = %s", rec.Body)
	}
	for _, bad := range []string{`1.234`, `"1"`, `25`, `-1`} {
		if rec := c.call("PUT", "/api/entries", `{"projectId":"`+p.ID+`","date":"2026-10-26","hours":`+bad+`}`); rec.Code != 400 {
			t.Errorf("hours %s: %d", bad, rec.Code)
		}
	}
	if rec := c.call("GET", "/api/week?start=2026-10-27", ""); rec.Code != 400 {
		t.Errorf("non-Monday week: %d", rec.Code)
	}
	if rec := c.call("GET", "/api/month?month=oct", ""); rec.Code != 400 {
		t.Errorf("bad month: %d", rec.Code)
	}
}

func TestAPIBackupFlow(t *testing.T) {
	c := newAPI(t)
	p := decode[store.Project](t, c.call("POST", "/api/projects", `{"name":"Acme"}`))
	c.call("PUT", "/api/entries", `{"projectId":"`+p.ID+`","date":"2026-10-26","hours":2}`)
	if st := decode[store.BackupStatus](t, c.call("GET", "/api/backup/status", "")); !st.Reminder {
		t.Errorf("status before download = %+v", st)
	}
	rec := c.call("GET", "/api/backup/download", "")
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), `filename="personal-time-tracker-backup-`) {
		t.Fatalf("download: %d %v", rec.Code, rec.Header())
	}
	backup := rec.Body.String()
	if st := decode[store.BackupStatus](t, c.call("GET", "/api/backup/status", "")); st.Reminder || st.LastDownloadAt == nil {
		t.Errorf("status after download = %+v", st)
	}
	rec = c.call("POST", "/api/backup/check", backup)
	if got := decode[map[string]int](t, rec); rec.Code != 200 || got["projects"] != 1 || got["entries"] != 1 {
		t.Errorf("check: %d %s", rec.Code, rec.Body)
	}
	if rec := c.call("POST", "/api/backup/restore", backup); rec.Code != 204 {
		t.Errorf("restore: %d %s", rec.Code, rec.Body)
	}
	if rec := c.call("POST", "/api/backup/check", `garbage`); rec.Code != 400 {
		t.Errorf("check garbage: %d", rec.Code)
	}
	if rec := c.call("POST", "/api/backup/restore", `{"version":1,"projects":[{"id":"a","name":"X","archived":false,"createdAt":"2026-10-01T00:00:00Z"},{"id":"b","name":"x","archived":false,"createdAt":"2026-10-01T00:00:00Z"}],"entries":[],"lastBackupDownloadAt":null}`); rec.Code != 400 {
		t.Errorf("restore duplicate names: %d, want 400", rec.Code)
	}
	snaps := decode[[]store.SnapshotInfo](t, c.call("GET", "/api/snapshots", ""))
	if len(snaps) != 1 || snaps[0].Kind != "pre-restore" {
		t.Fatalf("snapshots = %+v", snaps)
	}
	if rec := c.call("POST", "/api/snapshots/"+snaps[0].Name+"/restore", `{}`); rec.Code != 204 {
		t.Errorf("snapshot restore: %d %s", rec.Code, rec.Body)
	}
}

func TestAPISnapshotTraversal(t *testing.T) {
	c := newAPI(t)
	c.call("POST", "/api/projects", `{"name":"Acme"}`)
	path := filepath.Join(c.dir, "data.json")
	before, _ := os.ReadFile(path)
	for _, name := range []string{"..%2Fdata.json", "%2E%2E", "daily-x.json"} {
		rec := c.call("POST", "/api/snapshots/"+name+"/restore", `{}`)
		if rec.Code != 400 && rec.Code != 404 {
			t.Errorf("%s: %d, want 400/404", name, rec.Code)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("data.json changed")
	}
}

func TestAPIServesIndex(t *testing.T) {
	c := newAPI(t)
	rec := c.call("GET", "/", "")
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != 200 || !strings.Contains(string(body), "Personal Time Tracker") {
		t.Errorf("index: %d %s", rec.Code, body)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("index missing CSP")
	}
	if rec := c.call("GET", "/healthz", ""); rec.Code != 200 {
		t.Errorf("healthz: %d", rec.Code)
	}
}
