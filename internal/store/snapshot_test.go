package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func dailyFiles(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "snapshots", "daily-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDailySnapshotOncePerDay(t *testing.T) {
	s, dir, now := openTemp(t)
	if err := s.EnsureDailySnapshot(); err != nil {
		t.Fatal(err)
	}
	if n := len(dailyFiles(t, dir)); n != 0 {
		t.Fatalf("snapshot without data.json: %d files", n)
	}
	mustAdd(t, s, "Acme")
	s.EnsureDailySnapshot()
	s.EnsureDailySnapshot()
	if n := len(dailyFiles(t, dir)); n != 1 {
		t.Fatalf("same day: %d files, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "snapshots", "daily-2026-10-30.json")); err != nil {
		t.Error(err)
	}
	*now = now.Add(24 * time.Hour)
	s.EnsureDailySnapshot()
	if n := len(dailyFiles(t, dir)); n != 2 {
		t.Fatalf("next day: %d files, want 2", n)
	}
}

func TestDailySnapshotRotationKeeps14(t *testing.T) {
	s, dir, now := openTemp(t)
	mustAdd(t, s, "Acme")
	start := *now
	if err := s.writePreRestore([]byte("{}")); err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 20; day++ {
		*now = start.AddDate(0, 0, day)
		if err := s.EnsureDailySnapshot(); err != nil {
			t.Fatal(err)
		}
	}
	files := dailyFiles(t, dir)
	if len(files) != 14 {
		t.Fatalf("%d dailies, want 14", len(files))
	}
	if want := "daily-" + start.AddDate(0, 0, 6).Format("2006-01-02") + ".json"; filepath.Base(files[0]) != want {
		t.Errorf("oldest = %s, want %s", filepath.Base(files[0]), want)
	}
	if n := len(preRestoreFiles(t, dir)); n != 1 {
		t.Errorf("pre-restore files = %d, want 1", n)
	}
}

func TestSnapshotsListing(t *testing.T) {
	s, _, now := openTemp(t)
	mustAdd(t, s, "Acme")
	s.EnsureDailySnapshot() // 2026-10-30
	*now = now.Add(24*time.Hour + 3*time.Hour)
	s.writePreRestore([]byte("{}")) // 2026-10-31T15:00:00Z
	os.WriteFile(filepath.Join(s.dir, "snapshots", "notes.txt"), []byte("x"), 0o600)

	list, err := s.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Name != "pre-restore-20261031T150000Z.json" || list[0].Kind != "pre-restore" ||
		!list[0].CreatedAt.Equal(time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("first = %+v", list[0])
	}
	if list[1].Name != "daily-2026-10-30.json" || list[1].Kind != "daily" ||
		!list[1].CreatedAt.Equal(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("second = %+v", list[1])
	}
}

func TestRestoreSnapshot(t *testing.T) {
	s, dir, now := openTemp(t)
	mustAdd(t, s, "Acme")
	s.EnsureDailySnapshot()
	snap, _ := os.ReadFile(filepath.Join(dir, "snapshots", "daily-2026-10-30.json"))
	mustAdd(t, s, "Later")
	*now = now.Add(time.Hour)
	if err := s.RestoreSnapshot("daily-2026-10-30.json"); err != nil {
		t.Fatal(err)
	}
	if ps := s.Projects(); len(ps) != 1 || ps[0].Name != "Acme" {
		t.Errorf("after restore = %+v", ps)
	}
	if cur, _ := os.ReadFile(filepath.Join(dir, "data.json")); !bytes.Equal(cur, snap) {
		t.Errorf("data.json doesn't match snapshot")
	}
	if n := len(preRestoreFiles(t, dir)); n != 1 {
		t.Errorf("pre-restore files = %d, want 1", n)
	}
}

func TestRestoreSnapshotRejectsBadNames(t *testing.T) {
	s, dir, _ := openTemp(t)
	mustAdd(t, s, "Acme")
	before, _ := os.ReadFile(filepath.Join(dir, "data.json"))
	for _, name := range []string{"../data.json", "/etc/passwd", "daily-x.json", "daily-2026-10-30.json/../../data.json", ""} {
		if err := s.RestoreSnapshot(name); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: err = %v, want ErrInvalid", name, err)
		}
	}
	if err := s.RestoreSnapshot("daily-2020-01-01.json"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: err = %v, want ErrNotFound", err)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "data.json")); !bytes.Equal(before, after) {
		t.Error("data.json changed")
	}
}
