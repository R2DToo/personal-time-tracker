package store

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// FormatVersion is the version written to data.json and backup files.
const FormatVersion = 1

const maxNameRunes = 100

var (
	ErrNotFound      = errors.New("not found")
	ErrDuplicateName = errors.New("duplicate name")
)

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"createdAt"`
}

type Entry struct {
	ProjectID string `json:"projectId"`
	Date      string `json:"date"`
	Hours     Hours  `json:"hours"`
}

// Data is the whole persisted state; data.json and backup files share it.
type Data struct {
	Version              int        `json:"version"`
	Projects             []Project  `json:"projects"`
	Entries              []Entry    `json:"entries"`
	LastBackupDownloadAt *time.Time `json:"lastBackupDownloadAt"`
}

func (d Data) clone() Data {
	c := d
	c.Projects = append([]Project(nil), d.Projects...)
	c.Entries = append([]Entry(nil), d.Entries...)
	if d.LastBackupDownloadAt != nil {
		t := *d.LastBackupDownloadAt
		c.LastBackupDownloadAt = &t
	}
	return c
}

func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errorf(ErrInvalid, "project name can't be empty")
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		return "", errorf(ErrInvalid, "project name can't be longer than %d characters", maxNameRunes)
	}
	return name, nil
}

// Validate checks every invariant of Data and returns the first problem.
func Validate(d Data) error {
	if d.Version != FormatVersion {
		return errorf(ErrInvalid, "unsupported format version %d", d.Version)
	}
	ids := make(map[string]bool, len(d.Projects))
	names := make(map[string]bool, len(d.Projects))
	for _, p := range d.Projects {
		if p.ID == "" {
			return errorf(ErrInvalid, "project with empty id")
		}
		if ids[p.ID] {
			return errorf(ErrInvalid, "duplicate project id %q", p.ID)
		}
		ids[p.ID] = true
		name, err := normalizeName(p.Name)
		if err != nil {
			return err
		}
		key := strings.ToLower(name)
		if names[key] {
			return errorf(ErrDuplicateName, "more than one project named %q", name)
		}
		names[key] = true
	}
	seen := make(map[[2]string]bool, len(d.Entries))
	for _, e := range d.Entries {
		if !ids[e.ProjectID] {
			return errorf(ErrInvalid, "entry for unknown project %q", e.ProjectID)
		}
		if _, err := ParseDate(e.Date); err != nil {
			return err
		}
		if e.Hours <= 0 || e.Hours > MaxHours {
			return errorf(ErrInvalid, "entry on %s has %s hours; must be more than 0 and at most 24", e.Date, e.Hours)
		}
		k := [2]string{e.ProjectID, e.Date}
		if seen[k] {
			return errorf(ErrInvalid, "more than one entry for project %q on %s", e.ProjectID, e.Date)
		}
		seen[k] = true
	}
	return nil
}
