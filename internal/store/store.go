package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	dataFile     = "data.json"
	snapshotsDir = "snapshots"
)

// Store owns data.json in dir. All methods are safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	dir  string
	now  func() time.Time
	data Data
}

// Open loads dir/data.json, or starts empty if it doesn't exist. A file
// that fails to decode or validate is an error and is never overwritten.
func Open(dir string, now func() time.Time) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, snapshotsDir), 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, now: now, data: Data{Version: FormatVersion}}
	raw, err := os.ReadFile(filepath.Join(dir, dataFile))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	d, err := decodeData(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is unreadable; fix or restore it before starting: %w", filepath.Join(dir, dataFile), err)
	}
	s.data = d
	return s, nil
}

// decodeData strictly decodes and validates a data or backup file.
func decodeData(raw []byte) (Data, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var d Data
	if err := dec.Decode(&d); err != nil {
		return Data{}, errorf(ErrInvalid, "not a valid Personal Time Tracker file: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Data{}, errorf(ErrInvalid, "not a valid Personal Time Tracker file: unexpected data after the JSON document")
	}
	if err := Validate(d); err != nil {
		// Every problem in a file, duplicate names included, is invalid input.
		return Data{}, asInvalid(err)
	}
	// Store names in canonical trimmed form so later uniqueness checks
	// compare like with like.
	for i := range d.Projects {
		d.Projects[i].Name = strings.TrimSpace(d.Projects[i].Name)
	}
	return d, nil
}

func encodeData(d Data) ([]byte, error) {
	if d.Projects == nil {
		d.Projects = []Project{}
	}
	if d.Entries == nil {
		d.Entries = []Entry{}
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// writeFileAtomic writes via a synced temp file and rename, then syncs the
// directory, so a crash leaves either the old or the new file intact.
func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// commit saves d and, only if that succeeds, makes it the live state.
// Call with s.mu held.
func (s *Store) commit(d Data) error {
	// Never write a state the next Open would refuse to load.
	if err := Validate(d); err != nil {
		return err
	}
	b, err := encodeData(d)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(s.dir, dataFile), b); err != nil {
		return err
	}
	s.data = d
	return nil
}

func sortProjects(ps []Project) {
	sort.SliceStable(ps, func(i, j int) bool {
		return strings.ToLower(ps[i].Name) < strings.ToLower(ps[j].Name)
	})
}

// Projects returns all projects sorted by name, ignoring case.
func (s *Store) Projects() []Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps := append([]Project{}, s.data.Projects...)
	sortProjects(ps)
	return ps
}

func (s *Store) projectIndex(d Data, id string) int {
	for i, p := range d.Projects {
		if p.ID == id {
			return i
		}
	}
	return -1
}

func nameTaken(d Data, name, exceptID string) bool {
	for _, p := range d.Projects {
		if p.ID != exceptID && nameKey(p.Name) == nameKey(name) {
			return true
		}
	}
	return false
}

func newProjectID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "p_" + hex.EncodeToString(b[:]), nil
}

func (s *Store) AddProject(name string) (Project, error) {
	name, err := normalizeName(name)
	if err != nil {
		return Project{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if nameTaken(s.data, name, "") {
		return Project{}, errorf(ErrDuplicateName, "a project named %q already exists", name)
	}
	id, err := newProjectID()
	if err != nil {
		return Project{}, err
	}
	p := Project{ID: id, Name: name, CreatedAt: s.now().UTC()}
	d := s.data.clone()
	d.Projects = append(d.Projects, p)
	if err := s.commit(d); err != nil {
		return Project{}, err
	}
	return p, nil
}

func (s *Store) RenameProject(id, name string) (Project, error) {
	name, err := normalizeName(name)
	if err != nil {
		return Project{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.projectIndex(s.data, id)
	if i < 0 {
		return Project{}, errorf(ErrNotFound, "project %q", id)
	}
	if nameTaken(s.data, name, id) {
		return Project{}, errorf(ErrDuplicateName, "a project named %q already exists", name)
	}
	d := s.data.clone()
	d.Projects[i].Name = name
	if err := s.commit(d); err != nil {
		return Project{}, err
	}
	return d.Projects[i], nil
}

func (s *Store) SetArchived(id string, archived bool) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.projectIndex(s.data, id)
	if i < 0 {
		return Project{}, errorf(ErrNotFound, "project %q", id)
	}
	d := s.data.clone()
	d.Projects[i].Archived = archived
	if err := s.commit(d); err != nil {
		return Project{}, err
	}
	return d.Projects[i], nil
}
