package browsercredential

import "testing"

func TestMaskUsername(t *testing.T) {
	tests := map[string]string{
		"alice@example.com": "al***@example.com",
		"ab@example.com":    "a***@example.com",
		"athena-user":       "at***r",
		"a":                 "a***",
	}
	for input, want := range tests {
		if got := maskUsername(input); got != want {
			t.Fatalf("maskUsername(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateLoginURL(t *testing.T) {
	for _, input := range []string{"javascript:alert(1)", "/login", "https://user:pass@example.com"} {
		if _, err := validateLoginURL(input); err == nil {
			t.Fatalf("validateLoginURL(%q) accepted unsafe URL", input)
		}
	}
	if _, err := validateLoginURL("https://example.com/login"); err != nil {
		t.Fatalf("valid URL rejected: %v", err)
	}
}

func TestNormalizeDomain(t *testing.T) {
	for input, want := range map[string]string{
		"https://www.Example.com/login": "example.com",
		"www.example.com":               "example.com",
		"accounts.example.com":          "accounts.example.com",
	} {
		if got := normalizeDomain(input); got != want {
			t.Fatalf("normalizeDomain(%q) = %q, want %q", input, got, want)
		}
	}
}
