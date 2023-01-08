package csb

import (
	"errors"
	"fmt"
	"net/http"
)

const (
	ECONFLICT       = "conflict"
	EINTERNAL       = "internal"
	EINVALID        = "invalid"
	ENOTFOUND       = "not_found"
	ENOTIMPLEMENTED = "not_implemented"
	EUNAUTHORIZED   = "unauthorized"
)

type Error struct {
	// Machine-readable error code.
	Code string

	// Human-readable error message.
	Message string
}

// Error implements the error interface. Not used by the application otherwise.
func (e *Error) Error() string {
	return fmt.Sprintf("csb error: code=%s message=%s", e.Code, e.Message)
}

func ErrorCode(err error) string {
	var e *Error
	if err == nil {
		return ""
	} else if errors.As(err, &e) {
		return e.Code
	}
	return EINTERNAL
}

func ErrorMessage(err error) string {
	var e *Error
	if err == nil {
		return ""
	} else if errors.As(err, &e) {
		return e.Code
	}
	return "internal error"
}

func Errorf(code string, format string, args ...interface{}) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

var codes = map[string]int{
	ECONFLICT:       http.StatusConflict,
	EINVALID:        http.StatusBadRequest,
	ENOTFOUND:       http.StatusNotFound,
	ENOTIMPLEMENTED: http.StatusNotImplemented,
	EUNAUTHORIZED:   http.StatusUnauthorized,
	EINTERNAL:       http.StatusInternalServerError,
}

// FromErrorCodeToStatus maps a csb error code to a http status code, if no mapping is possible
// status code 500 is returned.
func FromErrorCodeToStatus(code string) int {
	if v, ok := codes[code]; ok {
		return v
	}
	return http.StatusInternalServerError
}

// FromStatusToErrorCode maps a http status code to a csb error code, if no mapping is possible
// csb.EINTERNAL is returned.
func FromStatusToErrorCode(code int) string {
	for k, v := range codes {
		if v == code {
			return k
		}
	}
	return EINTERNAL
}
