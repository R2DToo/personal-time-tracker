package store

import (
	"errors"
	"testing"
	"time"
)

func validData() Data {
	created := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return Data{
		Version: FormatVersion,
		Projects: []Project{
			{ID: "p_1", Name: "Acme", CreatedAt: created},
			{ID: "p_2", Name: "Globex", Archived: true, CreatedAt: created},
		},
		Entries: []Entry{
			{ProjectID: "p_1", Date: "2026-10-26", Hours: 400},
			{ProjectID: "p_2", Date: "2026-10-26", Hours: 150},
		},
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(validData()); err != nil {
		t.Fatalf("valid data: %v", err)
	}
	invalid := map[string]func(d *Data){
		"version 2":       func(d *Data) { d.Version = 2 },
		"empty id":        func(d *Data) { d.Projects[0].ID = "" },
		"duplicate id":    func(d *Data) { d.Projects[1].ID = "p_1" },
		"empty name":      func(d *Data) { d.Projects[0].Name = "  " },
		"unknown project": func(d *Data) { d.Entries[0].ProjectID = "p_9" },
		"bad date":        func(d *Data) { d.Entries[0].Date = "2026-13-01" },
		"zero hours":      func(d *Data) { d.Entries[0].Hours = 0 },
		"too many hours":  func(d *Data) { d.Entries[0].Hours = 2401 },
		"duplicate entry": func(d *Data) { d.Entries[1].ProjectID = "p_1" },
		"negative hours":  func(d *Data) { d.Entries[0].Hours = -5 },
	}
	for name, mutate := range invalid {
		d := validData()
		mutate(&d)
		if err := Validate(d); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	d := validData()
	d.Projects[0].Name = "Acme"
	d.Projects[1].Name = " acme "
	if err := Validate(d); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("duplicate names: err = %v, want ErrDuplicateName", err)
	}
}
