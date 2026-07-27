package config

import "testing"

func TestAddressPort(t *testing.T) {
	tests := map[string]int{
		":8090":                  8090,
		"127.0.0.1:18081":        18081,
		"http://127.0.0.1:18081": 18081,
	}
	for address, expected := range tests {
		port, err := addressPort(address)
		if err != nil {
			t.Fatalf("addressPort(%q) error = %v", address, err)
		}
		if port != expected {
			t.Fatalf("addressPort(%q) = %d, want %d", address, port, expected)
		}
	}
}

func TestAddressPortRejectsInvalidValues(t *testing.T) {
	for _, address := range []string{"", "localhost", ":0", ":70000"} {
		if _, err := addressPort(address); err == nil {
			t.Fatalf("addressPort(%q) expected an error", address)
		}
	}
}
