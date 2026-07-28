// Package errtrace preserves an error chain together with the source location
// of each layer that adds operational context.
package errtrace

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

type Error struct {
	Operation string
	File      string
	Line      int
	Err       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Operation == "" {
		return e.Err.Error()
	}
	return e.Operation + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

func Wrap(err error, operation string) error {
	return wrapAt(err, operation, 1)
}

func Errorf(operation, format string, args ...any) error {
	return wrapAt(fmt.Errorf(format, args...), operation, 1)
}

func wrapAt(err error, operation string, skip int) error {
	if err == nil {
		return nil
	}
	_, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		file = "unknown"
	}
	return &Error{Operation: operation, File: normalizeFile(file), Line: line, Err: err}
}

func Format(err error) string {
	if err == nil {
		return ""
	}
	var frames []string
	for current := err; current != nil; current = errors.Unwrap(current) {
		var traced *Error
		if !errors.As(current, &traced) || traced != current {
			continue
		}
		frames = append(frames, fmt.Sprintf("at %s (%s:%d)", traced.Operation, traced.File, traced.Line))
	}
	if len(frames) == 0 {
		return err.Error()
	}
	return err.Error() + "\n" + strings.Join(frames, "\n")
}

func normalizeFile(file string) string {
	file = strings.ReplaceAll(file, "\\", "/")
	for _, marker := range []string{"/agent-runtime/", "/agent-runtime-client/"} {
		if index := strings.LastIndex(file, marker); index >= 0 {
			return strings.TrimPrefix(file[index+1:], "/")
		}
	}
	return file
}
