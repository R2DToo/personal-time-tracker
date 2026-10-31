package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestParseHours(t *testing.T) {
	valid := map[string]Hours{
		"4":    400,
		"1.5":  150,
		"0.25": 25,
		".5":   50,
		"24":   2400,
		"0":    0,
	}
	for in, want := range valid {
		got, err := ParseHours(in)
		if err != nil {
			t.Errorf("ParseHours(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseHours(%q) = %d, want %d", in, got, want)
		}
	}
	invalid := []string{"", "-1", "24.01", "1.234", "1e2", "abc", "1.5.5", " 1", "."}
	for _, in := range invalid {
		if _, err := ParseHours(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseHours(%q) err = %v, want ErrInvalid", in, err)
		}
	}
}

func TestHoursString(t *testing.T) {
	cases := map[Hours]string{150: "1.5", 400: "4", 25: "0.25", 1010: "10.1", 0: "0", 5: "0.05"}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("Hours(%d).String() = %q, want %q", in, got, want)
		}
	}
}

func TestHoursRoundTrip(t *testing.T) {
	var h Hours
	if err := json.Unmarshal([]byte("0.29"), &h); err != nil {
		t.Fatal(err)
	}
	if h != 29 {
		t.Fatalf("unmarshal 0.29 = %d, want 29", h)
	}
	if got := (h + h + h).String(); got != "0.87" {
		t.Errorf("0.29*3 = %q, want 0.87", got)
	}
	b, err := json.Marshal(Hours(87))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "0.87" {
		t.Errorf("marshal = %s, want 0.87", b)
	}
	for _, raw := range []string{`"1"`, `null`, `1.234`, `-1`} {
		var x Hours
		if err := json.Unmarshal([]byte(raw), &x); err == nil {
			t.Errorf("unmarshal %s: want error", raw)
		}
	}
}

func TestParseDate(t *testing.T) {
	d, err := ParseDate("2026-10-26")
	if err != nil {
		t.Fatal(err)
	}
	if d.Weekday() != time.Monday {
		t.Errorf("2026-10-26 weekday = %v, want Monday", d.Weekday())
	}
	if d.Location() != time.UTC {
		t.Errorf("location = %v, want UTC", d.Location())
	}
	for _, in := range []string{"2026-02-30", "2026-1-05", "26-10-26", ""} {
		if _, err := ParseDate(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseDate(%q) err = %v, want ErrInvalid", in, err)
		}
	}
}
