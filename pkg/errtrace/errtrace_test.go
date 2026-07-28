package errtrace

import (
	"errors"
	"strings"
	"testing"
)

func TestWrapPreservesChainAndLocations(t *testing.T) {
	sentinel := errors.New("database unavailable")
	err := Wrap(Wrap(sentinel, "repository.UpdateUser"), "SysUserService.Update")
	if !errors.Is(err, sentinel) {
		t.Fatal("wrapped error no longer matches sentinel")
	}
	detail := Format(err)
	for _, expected := range []string{"SysUserService.Update: repository.UpdateUser: database unavailable", "at SysUserService.Update", "at repository.UpdateUser", "errtrace_test.go:"} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("Format() missing %q:\n%s", expected, detail)
		}
	}
}

func TestWrapNil(t *testing.T) {
	if Wrap(nil, "operation") != nil {
		t.Fatal("Wrap(nil) must return nil")
	}
}
