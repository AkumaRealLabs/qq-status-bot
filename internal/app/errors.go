package app

import (
	"errors"
	"net/http"
)

type badRequestError struct {
	err error
}

type statusError struct {
	status int
	err    error
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

func ErrStatus(status int, msg string) error {
	return statusError{status: status, err: errors.New(msg)}
}

var ErrOneBotUnauthorized = ErrStatus(http.StatusUnauthorized, "unauthorized")

func ErrorStatus(err error) (int, bool) {
	var target statusError
	if errors.As(err, &target) {
		return target.status, true
	}
	return 0, false
}

func (e badRequestError) Error() string { return e.err.Error() }
func (e badRequestError) Unwrap() error { return e.err }
func (e statusError) Error() string     { return e.err.Error() }
func (e statusError) Unwrap() error     { return e.err }
