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
	return ListSnapshots(s.dir)
}

// ListSnapshots lists the snapshots in a data dir, newest first. It works
// without opening the store, so it's usable when data.json is unreadable.
// CreatedAt is the file's write time: the real moment of the snapshot.
func ListSnapshots(dataDir string) ([]SnapshotInfo, error) {
	entries, err := os.ReadDir(filepath.Join(dataDir, snapshotsDir))
	if err != nil {
		return nil, err
	}
	list := []SnapshotInfo{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !snapshotName.MatchString(name) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		kind := "pre-restore"
		if strings.HasPrefix(name, "daily-") {
			kind = "daily"
		}
		list = append(list, SnapshotInfo{Name: name, Kind: kind, CreatedAt: fi.ModTime().UTC()})
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].CreatedAt.After(list[j].CreatedAt)
		}
		return list[i].Name > list[j].Name
	})
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

// RecoverFromSnapshot replaces dataDir/data.json with a snapshot without
// opening the store, for when data.json is unreadable and the app won't
// start. The current data.json (if any) is kept aside, and its new path is
// returned. Nothing changes unless the snapshot is valid.
func RecoverFromSnapshot(dataDir, name string, now func() time.Time) (keptAside string, err error) {
	if !snapshotName.MatchString(name) {
		return "", errorf(ErrInvalid, "not a snapshot name: %q", name)
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, snapshotsDir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return "", errorf(ErrNotFound, "snapshot %q not found", name)
	}
	if err != nil {
		return "", err
	}
	if _, err := DecodeBackup(raw); err != nil {
		return "", err
	}
	path := filepath.Join(dataDir, dataFile)
	if _, err := os.Stat(path); err == nil {
		keptAside = path + ".broken-" + now().UTC().Format("20060102T150405Z")
		if err := os.Rename(path, keptAside); err != nil {
			return "", err
		}
	}
	return keptAside, writeFileAtomic(path, raw)
}
