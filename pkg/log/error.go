package log

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var errorSourceRoot struct {
	sync.RWMutex
	path string
}

// SetErrorSourceRoot controls how source paths are shortened by FormatError.
// An empty root uses the process working directory.
func SetErrorSourceRoot(root string) {
	root = strings.TrimSpace(root)
	if root != "" {
		if absolute, err := filepath.Abs(root); err == nil {
			root = absolute
		}
	}
	errorSourceRoot.Lock()
	errorSourceRoot.path = root
	errorSourceRoot.Unlock()
}

// TracedError is one contextual source frame in an error chain.
type TracedError struct {
	Operation string
	File      string
	Line      int
	Err       error
}

func (e *TracedError) Error() string {
	if e == nil {
		return ""
	}
	if e.Operation == "" {
		return e.Err.Error()
	}
	return e.Operation + ": " + e.Err.Error()
}

func (e *TracedError) Unwrap() error { return e.Err }

// WrapError adds operation context and its call site while preserving errors.Is/As.
func WrapError(err error, operation string) error {
	return wrapErrorAt(err, operation, 1)
}

// NewError constructs an error and captures its construction site.
func NewError(operation, format string, args ...any) error {
	return wrapErrorAt(fmt.Errorf(format, args...), operation, 1)
}

func wrapErrorAt(err error, operation string, skip int) error {
	if err == nil {
		return nil
	}
	_, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		file = "unknown"
	}
	return &TracedError{Operation: operation, File: normalizeErrorFile(file), Line: line, Err: err}
}

// FormatError returns the error message followed by every captured source frame.
func FormatError(err error) string {
	if err == nil {
		return ""
	}
	var frames []string
	for current := err; current != nil; current = errors.Unwrap(current) {
		var traced *TracedError
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

func normalizeErrorFile(file string) string {
	errorSourceRoot.RLock()
	root := errorSourceRoot.path
	errorSourceRoot.RUnlock()
	if root == "" {
		root, _ = os.Getwd()
	}
	if root != "" {
		if relative, err := filepath.Rel(root, file); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(relative)
		}
	}
	return filepath.ToSlash(file)
}
