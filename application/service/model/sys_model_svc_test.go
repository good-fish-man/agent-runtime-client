package model

import "testing"

func TestMaskKey(t *testing.T) {
	tests := map[string]string{
		"":              "未设置",
		"short":         "********",
		"sk-1234567890": "sk-1...7890",
	}
	for input, want := range tests {
		if got := maskKey(input); got != want {
			t.Fatalf("maskKey(%q) = %q, want %q", input, got, want)
		}
	}
}
