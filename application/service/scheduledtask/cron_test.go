package scheduledtask

import (
	"testing"
	"time"
)

func TestCronMatches(t *testing.T) {
	at := time.Date(2026, time.July, 31, 10, 15, 0, 0, time.UTC)
	for expr, want := range map[string]bool{
		"15 10 * * *":   true,
		"*/5 10 * * *":  true,
		"0 10 * * *":    false,
		"15 9-11 * * 5": true,
	} {
		if got := cronMatches(expr, at); got != want {
			t.Fatalf("cronMatches(%q) = %v, want %v", expr, got, want)
		}
	}
}

func TestValidateCronRejectsInvalid(t *testing.T) {
	for _, expr := range []string{"* * *", "60 * * * *", "*/0 * * * *", "* * * * 7"} {
		if err := validateCron(expr); err == nil {
			t.Fatalf("validateCron(%q) succeeded", expr)
		}
	}
}
