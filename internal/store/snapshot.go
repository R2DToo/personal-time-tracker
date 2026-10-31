package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const keepDailySnapshots = 14

var snapshotName = regexp.MustCompile(`^(daily-\d{4}-\d{2}-\d{2}|pre-restore-\d{8}T\d{6}Z)\.json$`)

type SnapshotInfo struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // "daily" or "pre-restore"
	CreatedAt time.Time `json:"createdAt"`
}

// EnsureDailySnapshot copies data.json to snapshots/daily-<today>.json if
// today's copy doesn't exist yet, then prunes dailies beyond the newest 14.
// Pre-restore snapshots are never pruned.
func (s *Store) EnsureDailySnapshot() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := os.ReadFile(filepath.Join(s.dir, dataFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	dir := filepath.Join(s.dir, snapshotsDir)
	today := filepath.Join(dir, "daily-"+s.now().UTC().Format(dateLayout)+".json")
	if _, err := os.Stat(today); errors.Is(err, fs.ErrNotExist) {
		if err := writeFileAtomic(today, cur); err != nil {
			return err
		}
	}
	dailies, err := filepath.Glob(filepath.Join(dir, "daily-*.json"))
	if err != nil {
		return err
	}
	sort.Strings(dailies)
	for len(dailies) > keepDailySnapshots {
		if err := os.Remove(dailies[0]); err != nil {
			return err
		}
		dailies = dailies[1:]
	}
	return nil
}

// Snapshots lists restorable snapshots, newest first.
func (s *Store) Snapshots() ([]SnapshotInfo, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, snapshotsDir))
	if err != nil {
		return nil, err
	}
	list := []SnapshotInfo{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !snapshotName.MatchString(name) {
			continue
		}
		info := SnapshotInfo{Name: name}
		stem := strings.TrimSuffix(name, ".json")
		if d, ok := strings.CutPrefix(stem, "daily-"); ok {
			info.Kind = "daily"
			info.CreatedAt, err = time.Parse(dateLayout, d)
		} else {
			info.Kind = "pre-restore"
			info.CreatedAt, err = time.Parse("20060102T150405Z", strings.TrimPrefix(stem, "pre-restore-"))
		}
		if err != nil {
			continue // name matched the pattern but isn't a real date
		}
		list = append(list, info)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	return list, nil
}

// RestoreSnapshot replaces all data with a snapshot, after saving the
// current data as a pre-restore snapshot.
func (s *Store) RestoreSnapshot(name string) error {
	if !snapshotName.MatchString(name) {
		return errorf(ErrInvalid, "not a snapshot name: %q", name)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, snapshotsDir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return errorf(ErrNotFound, "snapshot %q", name)
	}
	if err != nil {
		return err
	}
	d, err := DecodeBackup(raw)
	if err != nil {
		return err
	}
	return s.restoreData(d)
}
