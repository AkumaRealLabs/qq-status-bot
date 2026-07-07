package app

import (
	"errors"
)

type badRequestError struct {
	err error
}

func ErrBadRequest(msg string) error {
	return badRequestError{err: errors.New(msg)}
}

func BadRequest(err error) error {
	if err == nil || IsBadRequest(err) {
		return err
	}
	return badRequestError{err: err}
}

func IsBadRequest(err error) bool {
	var target badRequestError
	return errors.As(err, &target)
}

func (e badRequestError) Error() string { return e.err.Error() }
func (e badRequestError) Unwrap() error { return e.err }
