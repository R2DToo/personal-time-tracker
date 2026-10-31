package store

import (
	"strings"
	"time"
)

// SetHours records h hours for a project on a date; 0 removes the entry.
// Archived projects still accept hours so past weeks stay editable.
func (s *Store) SetHours(projectID, date string, h Hours) error {
	if _, err := ParseDate(date); err != nil {
		return err
	}
	if h < 0 || h > MaxHours {
		return errorf(ErrInvalid, "hours must be between 0 and 24")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.projectIndex(s.data, projectID) < 0 {
		return errorf(ErrNotFound, "project %q", projectID)
	}
	d := s.data.clone()
	found := -1
	for i, e := range d.Entries {
		if e.ProjectID == projectID && e.Date == date {
			found = i
			break
		}
	}
	switch {
	case found >= 0 && h == 0:
		d.Entries = append(d.Entries[:found], d.Entries[found+1:]...)
	case found >= 0:
		d.Entries[found].Hours = h
	case h == 0:
		return nil
	default:
		d.Entries = append(d.Entries, Entry{ProjectID: projectID, Date: date, Hours: h})
	}
	return s.commit(d)
}

type WeekRow struct {
	ProjectID string   `json:"projectId"`
	Name      string   `json:"name"`
	Archived  bool     `json:"archived"`
	Hours     [7]Hours `json:"hours"`
}

type WeekView struct {
	Start string    `json:"start"`
	Days  [7]string `json:"days"`
	Rows  []WeekRow `json:"rows"`
}

// Week returns the Monday-to-Sunday grid starting at start. Rows are every
// active project plus archived projects that have hours in that week.
func (s *Store) Week(start string) (WeekView, error) {
	t, err := ParseDate(start)
	if err != nil {
		return WeekView{}, err
	}
	if t.Weekday() != time.Monday {
		return WeekView{}, errorf(ErrInvalid, "week must start on a Monday, got %s", start)
	}
	w := WeekView{Start: start, Rows: []WeekRow{}}
	dayIndex := make(map[string]int, 7)
	for i := range w.Days {
		w.Days[i] = t.AddDate(0, 0, i).Format(dateLayout)
		dayIndex[w.Days[i]] = i
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	hours := make(map[string]*[7]Hours)
	for _, e := range s.data.Entries {
		if i, ok := dayIndex[e.Date]; ok {
			if hours[e.ProjectID] == nil {
				hours[e.ProjectID] = new([7]Hours)
			}
			hours[e.ProjectID][i] = e.Hours
		}
	}
	ps := append([]Project(nil), s.data.Projects...)
	sortProjects(ps)
	for _, p := range ps {
		h := hours[p.ID]
		if p.Archived && h == nil {
			continue
		}
		row := WeekRow{ProjectID: p.ID, Name: p.Name, Archived: p.Archived}
		if h != nil {
			row.Hours = *h
		}
		w.Rows = append(w.Rows, row)
	}
	return w, nil
}

type MonthRow struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Archived  bool   `json:"archived"`
	Hours     Hours  `json:"hours"`
}

type MonthView struct {
	Month string     `json:"month"`
	Rows  []MonthRow `json:"rows"`
	Total Hours      `json:"total"`
}

// Month totals hours per project for a calendar month (YYYY-MM). Only days
// inside the month count, so weeks spanning two months split by day.
func (s *Store) Month(month string) (MonthView, error) {
	if t, err := time.Parse("2006-01", month); err != nil || t.Format("2006-01") != month {
		return MonthView{}, errorf(ErrInvalid, "month must be YYYY-MM, got %q", month)
	}
	prefix := month + "-"
	m := MonthView{Month: month, Rows: []MonthRow{}}

	s.mu.Lock()
	defer s.mu.Unlock()
	totals := make(map[string]Hours)
	for _, e := range s.data.Entries {
		if strings.HasPrefix(e.Date, prefix) {
			totals[e.ProjectID] += e.Hours
		}
	}
	ps := append([]Project(nil), s.data.Projects...)
	sortProjects(ps)
	for _, p := range ps {
		if h := totals[p.ID]; h > 0 {
			m.Rows = append(m.Rows, MonthRow{ProjectID: p.ID, Name: p.Name, Archived: p.Archived, Hours: h})
			m.Total += h
		}
	}
	return m, nil
}
