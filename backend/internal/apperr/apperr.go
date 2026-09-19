// Package apperr defines user-facing application errors.
//
// Handlers return *Error values for expected failures. Anything else is
// treated as an internal error: it is logged server-side and the client only
// sees a generic message, so internal details never leak.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

func NotFound(resource string) *Error {
	return New(http.StatusNotFound, "not_found", resource+" not found")
}

func BadRequest(msg string) *Error { return New(http.StatusBadRequest, "bad_request", msg) }

func Unauthorized(msg string) *Error { return New(http.StatusUnauthorized, "unauthorized", msg) }

func Forbidden(msg string) *Error { return New(http.StatusForbidden, "forbidden", msg) }

func Conflict(msg string) *Error { return New(http.StatusConflict, "conflict", msg) }

func Unprocessable(code, msg string) *Error {
	return New(http.StatusUnprocessableEntity, code, msg)
}

func RateLimited() *Error {
	return New(http.StatusTooManyRequests, "rate_limited", "Too many requests. Please slow down and try again shortly.")
}

func Validation(fields map[string]string) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "Some fields are invalid.", Fields: fields}
}

func Internal() *Error {
	return New(http.StatusInternalServerError, "internal_error", "Something went wrong on our side. The error has been logged.")
}

// As extracts an *Error from err.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Wrapf is a convenience for fmt.Errorf that keeps call sites terse.
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(format+": %w", append(args, err)...)
}
