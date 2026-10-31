package store

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrInvalid marks input that fails validation; callers map it to a 400.
var ErrInvalid = errors.New("invalid")

// Hours is a duration in hundredths of an hour. Integer storage keeps
// sums exact (0.29 * 3 is 0.87, not 0.8699999).
type Hours int64

// MaxHours is the most a single day's cell may hold.
const MaxHours Hours = 2400

const dateLayout = "2006-01-02"

// ParseHours parses a non-negative decimal with at most two decimal places,
// no greater than 24.
func ParseHours(s string) (Hours, error) {
	whole, frac, hasDot := strings.Cut(s, ".")
	if (whole == "" && frac == "") || !allDigits(whole) || !allDigits(frac) || len(frac) > 2 || (hasDot && frac == "") {
		return 0, fmt.Errorf("%w: hours must be a number like 1.5 with at most two decimals", ErrInvalid)
	}
	var h int64
	if whole != "" {
		if len(whole) > 4 {
			return 0, fmt.Errorf("%w: hours must be between 0 and 24", ErrInvalid)
		}
		w, _ := strconv.ParseInt(whole, 10, 64)
		h = w * 100
	}
	if frac != "" {
		f, _ := strconv.ParseInt(frac, 10, 64)
		if len(frac) == 1 {
			f *= 10
		}
		h += f
	}
	if Hours(h) > MaxHours {
		return 0, fmt.Errorf("%w: hours must be between 0 and 24", ErrInvalid)
	}
	return Hours(h), nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// String renders the shortest decimal form: 150 → "1.5", 400 → "4".
func (h Hours) String() string {
	whole, frac := int64(h)/100, int64(h)%100
	switch {
	case frac == 0:
		return strconv.FormatInt(whole, 10)
	case frac%10 == 0:
		return fmt.Sprintf("%d.%d", whole, frac/10)
	default:
		return fmt.Sprintf("%d.%02d", whole, frac)
	}
}

func (h Hours) MarshalJSON() ([]byte, error) {
	return []byte(h.String()), nil
}

// UnmarshalJSON parses the raw number token text so no float rounding
// ever happens; strings and null are rejected.
func (h *Hours) UnmarshalJSON(b []byte) error {
	v, err := ParseHours(string(b))
	if err != nil {
		return err
	}
	*h = v
	return nil
}

// ParseDate parses a strict YYYY-MM-DD calendar date at UTC midnight.
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil || t.Format(dateLayout) != s {
		return time.Time{}, fmt.Errorf("%w: date must be YYYY-MM-DD, got %q", ErrInvalid, s)
	}
	return t, nil
}
