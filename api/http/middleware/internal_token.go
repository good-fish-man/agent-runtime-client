package middleware

import (
	"crypto/subtle"
	"os"
	"strings"

	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

// InternalTokenValid authenticates machine-local service calls without
// exposing the launcher token through user-facing sessions.
func InternalTokenValid(value string) bool {
	want := strings.TrimSpace(os.Getenv(consts.EnvInternalServiceToken))
	value = strings.TrimSpace(value)
	return want != "" && len(value) == len(want) && subtle.ConstantTimeCompare([]byte(value), []byte(want)) == 1
}
