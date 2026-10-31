package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func preRestoreFiles(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "snapshots", "pre-restore-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestExportRoundTrip(t *testing.T) {
	s, _, now := openTemp(t)
	a := mustAdd(t, s, "Acme")
	mustSet(t, s, a.ID, "2026-10-26", 150)
	raw, name, err := s.ExportBackup()
	if err != nil {
		t.Fatal(err)
	}
	if name != "personal-time-tracker-backup-2026-10-30.json" {
		t.Errorf("filename = %q", name)
	}
	d, err := DecodeBackup(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d, s.data) {
		t.Errorf("decoded backup differs:\n%+v\n%+v", d, s.data)
	}
	st := s.BackupStatus()
	if st.LastDownloadAt == nil || !st.LastDownloadAt.Equal(*now) {
		t.Errorf("last download = %v", st.LastDownloadAt)
	}
}

func TestRestoreReplacesAndSnapshots(t *testing.T) {
	// Build the backup in a separate store.
	src, _, _ := openTemp(t)
	mustSet(t, src, mustAdd(t, src, "Globex").ID, "2026-10-01", 800)
	raw, _, err := src.ExportBackup()
	if err != nil {
		t.Fatal(err)
	}

	s, dir, now := openTemp(t)
	mustAdd(t, s, "Acme")
	*now = now.Add(-48 * time.Hour)
	if _, _, err := s.ExportBackup(); err != nil { // sets LastBackupDownloadAt
		t.Fatal(err)
	}
	keep := *s.data.LastBackupDownloadAt
	*now = now.Add(48 * time.Hour)
	old, _ := os.ReadFile(filepath.Join(dir, "data.json"))

	if err := s.Restore(raw); err != nil {
		t.Fatal(err)
	}
	if ps := s.Projects(); len(ps) != 1 || ps[0].Name != "Globex" || len(s.data.Entries) != 1 {
		t.Errorf("after restore = %+v", s.data)
	}
	snaps := preRestoreFiles(t, dir)
	if len(snaps) != 1 {
		t.Fatalf("pre-restore files = %v", snaps)
	}
	if got, _ := os.ReadFile(snaps[0]); !bytes.Equal(got, old) {
		t.Errorf("pre-restore snapshot doesn't hold the old data")
	}
	if !s.data.LastBackupDownloadAt.Equal(keep) {
		t.Errorf("lastBackupDownloadAt = %v, want %v", s.data.LastBackupDownloadAt, keep)
	}
}

func TestRestoreRejectsBadFiles(t *testing.T) {
	bad := map[string]string{
		"not json":        `not json`,
		"version 2":       `{"version":2,"projects":[],"entries":[],"lastBackupDownloadAt":null}`,
		"missing project": `{"version":1,"projects":[],"entries":[{"projectId":"p_1","date":"2026-10-01","hours":1}],"lastBackupDownloadAt":null}`,
		"duplicate entry": `{"version":1,"projects":[{"id":"p_1","name":"A","archived":false,"createdAt":"2026-10-01T00:00:00Z"}],"entries":[{"projectId":"p_1","date":"2026-10-01","hours":1},{"projectId":"p_1","date":"2026-10-01","hours":2}],"lastBackupDownloadAt":null}`,
		"hours 24.5":      `{"version":1,"projects":[{"id":"p_1","name":"A","archived":false,"createdAt":"2026-10-01T00:00:00Z"}],"entries":[{"projectId":"p_1","date":"2026-10-01","hours":24.5}],"lastBackupDownloadAt":null}`,
		"hours 1.234":     `{"version":1,"projects":[{"id":"p_1","name":"A","archived":false,"createdAt":"2026-10-01T00:00:00Z"}],"entries":[{"projectId":"p_1","date":"2026-10-01","hours":1.234}],"lastBackupDownloadAt":null}`,
		"duplicate names": `{"version":1,"projects":[{"id":"p_1","name":"A","archived":false,"createdAt":"2026-10-01T00:00:00Z"},{"id":"p_2","name":"a","archived":false,"createdAt":"2026-10-01T00:00:00Z"}],"entries":[],"lastBackupDownloadAt":null}`,
		"unknown field":   `{"version":1,"projects":[],"entries":[],"lastBackupDownloadAt":null,"extra":true}`,
		"trailing data":   `{"version":1,"projects":[],"entries":[],"lastBackupDownloadAt":null} {}`,
		"empty":           ``,
	}
	for name, raw := range bad {
		s, dir, _ := openTemp(t)
		mustAdd(t, s, "Keep")
		before, _ := os.ReadFile(filepath.Join(dir, "data.json"))
		err := s.Restore([]byte(raw))
		if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrDuplicateName) {
			t.Errorf("%s: err = %v, want ErrInvalid/ErrDuplicateName", name, err)
		}
		if after, _ := os.ReadFile(filepath.Join(dir, "data.json")); !bytes.Equal(before, after) {
			t.Errorf("%s: data.json changed", name)
		}
		if snaps := preRestoreFiles(t, dir); len(snaps) != 0 {
			t.Errorf("%s: pre-restore snapshot created: %v", name, snaps)
		}
	}
}

func TestBackupReminder(t *testing.T) {
	s, _, now := openTemp(t)
	if s.BackupStatus().Reminder {
		t.Error("reminder with no projects")
	}
	mustAdd(t, s, "Acme")
	if !s.BackupStatus().Reminder {
		t.Error("no reminder when never downloaded")
	}
	start := *now
	if _, _, err := s.ExportBackup(); err != nil {
		t.Fatal(err)
	}
	*now = start.Add(29 * 24 * time.Hour)
	if s.BackupStatus().Reminder {
		t.Error("reminder after 29 days")
	}
	*now = start.Add(31 * 24 * time.Hour)
	if !s.BackupStatus().Reminder {
		t.Error("no reminder after 31 days")
	}
}

// A backup with a padded name must not let a case/space duplicate in,
// which would leave a data.json the next Open refuses.
func TestRestorePaddedNameStaysLoadable(t *testing.T) {
	s, dir, now := openTemp(t)
	raw := `{"version":1,"projects":[{"id":"p_1","name":" Acme ","archived":false,"createdAt":"2026-10-01T00:00:00Z"}],"entries":[],"lastBackupDownloadAt":null}`
	if err := s.Restore([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if ps := s.Projects(); ps[0].Name != "Acme" {
		t.Errorf("restored name = %q, want trimmed", ps[0].Name)
	}
	if _, err := s.AddProject("acme"); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("add duplicate of padded name: err = %v", err)
	}
	if _, err := Open(dir, fakeClock(now)); err != nil {
		t.Errorf("reopen: %v", err)
	}
}
