package store

import (
	"errors"
	"fmt"
)

// kindError carries a user-facing message and one or more sentinel kinds
// (ErrInvalid, ErrNotFound, ErrDuplicateName) for errors.Is, without
// prefixing the sentinel's text onto the message.
type kindError struct {
	kinds []error
	msg   string
}

func (e *kindError) Error() string   { return e.msg }
func (e *kindError) Unwrap() []error { return e.kinds }

func errorf(kind error, format string, args ...any) error {
	return &kindError{kinds: []error{kind}, msg: fmt.Sprintf(format, args...)}
}

// Errorf builds an error of the given kind for callers outside the package.
func Errorf(kind error, format string, args ...any) error {
	return errorf(kind, format, args...)
}

// asInvalid makes err also match ErrInvalid, keeping its message and kinds.
func asInvalid(err error) error {
	if errors.Is(err, ErrInvalid) {
		return err
	}
	return &kindError{kinds: []error{ErrInvalid, err}, msg: err.Error()}
}
