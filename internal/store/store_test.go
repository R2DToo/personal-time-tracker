package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeClock returns a now func pinned to *t, so tests can move time.
func fakeClock(t *time.Time) func() time.Time { return func() time.Time { return *t } }

func openTemp(t *testing.T) (*Store, string, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	s, err := Open(dir, fakeClock(&now))
	if err != nil {
		t.Fatal(err)
	}
	return s, dir, &now
}

func TestAddRenameArchive(t *testing.T) {
	s, _, _ := openTemp(t)
	p, err := s.AddProject("  Acme  ")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Acme" {
		t.Errorf("name = %q, want Acme", p.Name)
	}
	if !regexp.MustCompile(`^p_[0-9a-f]{16}$`).MatchString(p.ID) {
		t.Errorf("id = %q", p.ID)
	}
	if _, err := s.AddProject("ACME"); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("dup add err = %v", err)
	}
	if _, err := s.AddProject(""); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty add err = %v", err)
	}
	if _, err := s.AddProject(strings.Repeat("é", 101)); !errors.Is(err, ErrInvalid) {
		t.Errorf("long add err = %v", err)
	}
	if _, err := s.AddProject(strings.Repeat("é", 100)); err != nil {
		t.Errorf("100-rune name: %v", err)
	}
	g, err := s.AddProject("zeta")
	if err != nil {
		t.Fatal(err)
	}
	if g, err = s.RenameProject(g.ID, "Globex"); err != nil || g.Name != "Globex" {
		t.Fatalf("rename = %v, %v", g, err)
	}
	if _, err := s.RenameProject(p.ID, "globex"); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("rename to dup err = %v", err)
	}
	if p2, err := s.RenameProject(p.ID, "ACME"); err != nil || p2.Name != "ACME" {
		t.Errorf("case-only rename = %v, %v", p2, err)
	}
	if a, err := s.SetArchived(p.ID, true); err != nil || !a.Archived {
		t.Errorf("archive = %v, %v", a, err)
	}
	if _, err := s.SetArchived("p_nope", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("archive unknown err = %v", err)
	}
	if _, err := s.RenameProject("p_nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename unknown err = %v", err)
	}
	names := []string{}
	for _, pr := range s.Projects() {
		names = append(names, pr.Name)
	}
	if names[0] != "ACME" || names[1] != "Globex" {
		t.Errorf("order = %v", names)
	}
}

func TestPersistsAcrossOpen(t *testing.T) {
	s, dir, now := openTemp(t)
	if _, err := s.AddProject("Acme"); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir, fakeClock(now))
	if err != nil {
		t.Fatal(err)
	}
	if ps := s2.Projects(); len(ps) != 1 || ps[0].Name != "Acme" {
		t.Errorf("reopened projects = %v", ps)
	}
	if _, err := os.Stat(filepath.Join(dir, "data.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("tmp file left behind: %v", err)
	}
}

func TestOpenCorruptFileFailsWithoutTouchingIt(t *testing.T) {
	for _, content := range []string{
		`{"version":1,"projects":[`,
		`{"version":1,"projects":[],"entries":[],"lastBackupDownloadAt":null,"foo":1}`,
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(dir, time.Now); err == nil {
			t.Errorf("Open(%q) succeeded, want error", content)
		}
		got, _ := os.ReadFile(path)
		if !bytes.Equal(got, []byte(content)) {
			t.Errorf("file modified: %q", got)
		}
	}
}

func TestOpenMissingFileDoesNotWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	if _, err := Open(dir, time.Now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data.json")); !os.IsNotExist(err) {
		t.Errorf("data.json created on open: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "snapshots")); err != nil || !fi.IsDir() {
		t.Errorf("snapshots dir missing: %v", err)
	}
}
