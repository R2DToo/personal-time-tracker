package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"

	"personaltimetracker/internal/store"
)

//go:embed static
var staticFiles embed.FS

// NewHandler returns the full app: JSON API under /api, the embedded UI
// at /, and /healthz, all behind guard.
func NewHandler(st *store.Store, port int) http.Handler {
	a := &api{st: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	mux.HandleFunc("GET /api/projects", a.listProjects)
	mux.HandleFunc("POST /api/projects", a.createProject)
	mux.HandleFunc("PATCH /api/projects/{id}", a.updateProject)
	mux.HandleFunc("GET /api/week", a.week)
	mux.HandleFunc("PUT /api/entries", a.setEntry)
	mux.HandleFunc("GET /api/month", a.month)
	mux.HandleFunc("GET /api/backup/status", a.backupStatus)
	mux.HandleFunc("GET /api/backup/download", a.backupDownload)
	mux.HandleFunc("POST /api/backup/check", a.backupCheck)
	mux.HandleFunc("POST /api/backup/restore", a.backupRestore)
	mux.HandleFunc("GET /api/snapshots", a.listSnapshots)
	mux.HandleFunc("POST /api/snapshots/{name}/restore", a.restoreSnapshot)
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServerFS(static))
	return guard(port, mux)
}

type api struct {
	st *store.Store
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request is too large"})
	case errors.Is(err, store.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrDuplicateName):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		slog.Error("request failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "something went wrong; check the container logs"})
	}
}

// decodeBody strictly decodes a JSON request body into v.
func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return err
		}
		return store.Errorf(store.ErrInvalid, "bad request: %v", err)
	}
	return nil
}

func (a *api) listProjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.st.Projects())
}

func (a *api) createProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	p, err := a.st.AddProject(req.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (a *api) updateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     *string `json:"name"`
		Archived *bool   `json:"archived"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if req.Name == nil && req.Archived == nil {
		writeErr(w, store.Errorf(store.ErrInvalid, "nothing to update"))
		return
	}
	id := r.PathValue("id")
	var p store.Project
	var err error
	if req.Name != nil {
		if p, err = a.st.RenameProject(id, *req.Name); err != nil {
			writeErr(w, err)
			return
		}
	}
	if req.Archived != nil {
		if p, err = a.st.SetArchived(id, *req.Archived); err != nil {
			writeErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *api) week(w http.ResponseWriter, r *http.Request) {
	v, err := a.st.Week(r.URL.Query().Get("start"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *api) setEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID string      `json:"projectId"`
		Date      string      `json:"date"`
		Hours     store.Hours `json:"hours"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.st.SetHours(req.ProjectID, req.Date, req.Hours); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) month(w http.ResponseWriter, r *http.Request) {
	v, err := a.st.Month(r.URL.Query().Get("month"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *api) backupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.st.BackupStatus())
}

func (a *api) backupDownload(w http.ResponseWriter, r *http.Request) {
	raw, name, err := a.st.ExportBackup()
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write(raw)
}

func readBackup(r *http.Request) (store.Data, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return store.Data{}, err
	}
	return store.DecodeBackup(bytes.TrimSpace(raw))
}

func (a *api) backupCheck(w http.ResponseWriter, r *http.Request) {
	d, err := readBackup(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"projects": len(d.Projects), "entries": len(d.Entries)})
}

func (a *api) backupRestore(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := a.st.Restore(bytes.TrimSpace(raw)); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listSnapshots(w http.ResponseWriter, r *http.Request) {
	list, err := a.st.Snapshots()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *api) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := a.st.RestoreSnapshot(r.PathValue("name")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
