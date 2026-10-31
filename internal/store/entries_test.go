package store

import (
	"errors"
	"testing"
)

func mustAdd(t *testing.T, s *Store, name string) Project {
	t.Helper()
	p, err := s.AddProject(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustSet(t *testing.T, s *Store, id, date string, h Hours) {
	t.Helper()
	if err := s.SetHours(id, date, h); err != nil {
		t.Fatal(err)
	}
}

func TestSetHours(t *testing.T) {
	s, _, _ := openTemp(t)
	a := mustAdd(t, s, "Acme")
	mustSet(t, s, a.ID, "2026-10-26", 400)
	mustSet(t, s, a.ID, "2026-10-26", 600)
	if e := s.data.Entries; len(e) != 1 || e[0].Hours != 600 {
		t.Fatalf("entries after overwrite = %v", e)
	}
	mustSet(t, s, a.ID, "2026-10-26", 0)
	if e := s.data.Entries; len(e) != 0 {
		t.Fatalf("entries after zero = %v", e)
	}
	if err := s.SetHours("p_nope", "2026-10-26", 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown project err = %v", err)
	}
	if err := s.SetHours(a.ID, "2026-10-32", 100); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad date err = %v", err)
	}
	if err := s.SetHours(a.ID, "2026-10-26", 2401); !errors.Is(err, ErrInvalid) {
		t.Errorf("too many hours err = %v", err)
	}
	if err := s.SetHours(a.ID, "2026-10-26", -1); !errors.Is(err, ErrInvalid) {
		t.Errorf("negative hours err = %v", err)
	}
	if _, err := s.SetArchived(a.ID, true); err != nil {
		t.Fatal(err)
	}
	mustSet(t, s, a.ID, "2026-10-27", 100)
}

func TestWeekView(t *testing.T) {
	s, _, _ := openTemp(t)
	a := mustAdd(t, s, "Acme")
	b := mustAdd(t, s, "Beta")
	c := mustAdd(t, s, "Cyan")
	mustSet(t, s, b.ID, "2026-10-28", 200)
	mustSet(t, s, c.ID, "2026-11-02", 100) // next week; C must not appear
	s.SetArchived(b.ID, true)
	s.SetArchived(c.ID, true)

	w, err := s.Week("2026-10-26")
	if err != nil {
		t.Fatal(err)
	}
	if w.Days[0] != "2026-10-26" || w.Days[6] != "2026-11-01" {
		t.Errorf("days = %v", w.Days)
	}
	if len(w.Rows) != 2 || w.Rows[0].ProjectID != a.ID || w.Rows[1].ProjectID != b.ID {
		t.Fatalf("rows = %+v", w.Rows)
	}
	if !w.Rows[1].Archived || w.Rows[1].Hours[2] != 200 {
		t.Errorf("archived row = %+v", w.Rows[1])
	}
	if _, err := s.Week("2026-10-27"); !errors.Is(err, ErrInvalid) {
		t.Errorf("non-Monday err = %v", err)
	}
}

func TestMonthSplitsYearBoundaryWeek(t *testing.T) {
	s, _, _ := openTemp(t)
	a := mustAdd(t, s, "Acme")
	for _, d := range []string{"2026-12-28", "2026-12-29", "2026-12-30", "2026-12-31", "2027-01-01", "2027-01-02", "2027-01-03"} {
		mustSet(t, s, a.ID, d, 800)
	}
	dec, err := s.Month("2026-12")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Total != 3200 {
		t.Errorf("Dec total = %s, want 32", dec.Total)
	}
	jan, _ := s.Month("2027-01")
	if jan.Total != 2400 {
		t.Errorf("Jan total = %s, want 24", jan.Total)
	}
}

func TestMonthTotalsExact(t *testing.T) {
	s, _, _ := openTemp(t)
	a := mustAdd(t, s, "Acme")
	h, err := ParseHours("0.29")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"2026-10-05", "2026-10-06", "2026-10-07"} {
		mustSet(t, s, a.ID, d, h)
	}
	m, _ := s.Month("2026-10")
	if got := m.Rows[0].Hours.String(); got != "0.87" {
		t.Errorf("total = %q, want 0.87", got)
	}
}

func TestMonthIncludesArchivedAndSkipsEmpty(t *testing.T) {
	s, _, _ := openTemp(t)
	a := mustAdd(t, s, "Acme")
	mustAdd(t, s, "Idle")
	mustSet(t, s, a.ID, "2026-10-05", 300)
	s.SetArchived(a.ID, true)
	m, err := s.Month("2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if m.Month != "2026-10" || len(m.Rows) != 1 || m.Rows[0].ProjectID != a.ID || !m.Rows[0].Archived || m.Total != 300 {
		t.Errorf("month = %+v", m)
	}
	if _, err := s.Month("2026-1"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad month err = %v", err)
	}
}
