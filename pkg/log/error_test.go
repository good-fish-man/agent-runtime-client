package log

import (
	"errors"
	"strings"
	"testing"
)

func TestWrapErrorPreservesChainAndLocations(t *testing.T) {
	sentinel := errors.New("database unavailable")
	err := WrapError(WrapError(sentinel, "repository.UpdateUser"), "SysUserService.Update")
	if !errors.Is(err, sentinel) {
		t.Fatal("wrapped error no longer matches sentinel")
	}
	detail := FormatError(err)
	for _, expected := range []string{"SysUserService.Update: repository.UpdateUser: database unavailable", "at SysUserService.Update", "at repository.UpdateUser", "error_test.go:"} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("FormatError() missing %q:\n%s", expected, detail)
		}
	}
}

func TestWrapErrorNil(t *testing.T) {
	if WrapError(nil, "operation") != nil {
		t.Fatal("WrapError(nil) must return nil")
	}
}
