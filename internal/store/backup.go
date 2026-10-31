package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const reminderAfter = 30 * 24 * time.Hour

// DecodeBackup strictly decodes and validates a backup file.
func DecodeBackup(raw []byte) (Data, error) {
	return decodeData(raw)
}

// ExportBackup records the download time and returns the saved data as a
// backup file plus its suggested filename.
func (s *Store) ExportBackup() ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	d := s.data.clone()
	d.LastBackupDownloadAt = &now
	if err := s.commit(d); err != nil {
		return nil, "", err
	}
	raw, err := encodeData(d)
	if err != nil {
		return nil, "", err
	}
	return raw, "personal-time-tracker-backup-" + now.Format(dateLayout) + ".json", nil
}

// Restore validates raw fully, then replaces all data with it. Nothing is
// written if validation fails.
func (s *Store) Restore(raw []byte) error {
	d, err := DecodeBackup(raw)
	if err != nil {
		return err
	}
	return s.restoreData(d)
}

// restoreData saves the current data.json as a pre-restore snapshot, then
// replaces the data with d, keeping the current backup-download time.
func (s *Store) restoreData(d Data) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := os.ReadFile(filepath.Join(s.dir, dataFile))
	switch {
	case err == nil:
		if err := s.writePreRestore(cur); err != nil {
			return fmt.Errorf("saving pre-restore snapshot: %w", err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	d = d.clone()
	d.LastBackupDownloadAt = s.data.clone().LastBackupDownloadAt
	return s.commit(d)
}

// writePreRestore stores b under a timestamped name, stepping forward a
// second on collision so an earlier pre-restore copy is never overwritten.
func (s *Store) writePreRestore(b []byte) error {
	t := s.now().UTC()
	for i := 0; i < 60; i++ {
		path := filepath.Join(s.dir, snapshotsDir, "pre-restore-"+t.Add(time.Duration(i)*time.Second).Format("20060102T150405Z")+".json")
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return writeFileAtomic(path, b)
		}
	}
	return errors.New("too many restores in the last minute")
}

type BackupStatus struct {
	LastDownloadAt *time.Time `json:"lastDownloadAt"`
	Reminder       bool       `json:"reminder"`
}

// BackupStatus reports the last download and whether to nudge the user:
// there is data and no backup has been downloaded in 30 days (or ever).
func (s *Store) BackupStatus() BackupStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	last := s.data.clone().LastBackupDownloadAt
	stale := last == nil || s.now().Sub(*last) > reminderAfter
	return BackupStatus{LastDownloadAt: last, Reminder: len(s.data.Projects) > 0 && stale}
}
