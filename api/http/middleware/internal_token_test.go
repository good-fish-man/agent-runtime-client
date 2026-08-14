package middleware

import (
	"testing"

	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

func TestInternalTokenValidFailsClosed(t *testing.T) {
	t.Setenv(consts.EnvInternalServiceToken, "")
	if InternalTokenValid("") || InternalTokenValid("anything") {
		t.Fatal("an unconfigured internal token must reject every request")
	}
}

func TestInternalTokenValidRequiresExactToken(t *testing.T) {
	t.Setenv(consts.EnvInternalServiceToken, "machine-local-secret")
	if !InternalTokenValid("machine-local-secret") {
		t.Fatal("the configured token was rejected")
	}
	for _, value := range []string{"machine-local", "machine-local-secret-extra", "MACHINE-LOCAL-SECRET"} {
		if InternalTokenValid(value) {
			t.Fatalf("non-exact token %q was accepted", value)
		}
	}
}
