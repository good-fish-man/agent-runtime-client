package config

import "testing"

func TestValidateYAMLAppConfig(t *testing.T) {
	t.Run("accepts known fields", func(t *testing.T) {
		content := "server:\n  http_addr: ':8090'\nruntime:\n  grpc_addr: localhost:18080\n"
		if err := validateYAML(content, true); err != nil {
			t.Fatalf("validateYAML() error = %v", err)
		}
	})

	t.Run("rejects unknown fields", func(t *testing.T) {
		content := "server:\n  public_port: 8090\n"
		if err := validateYAML(content, true); err == nil {
			t.Fatal("validateYAML() expected an unknown-field error")
		}
	})

	t.Run("rejects non-mapping root", func(t *testing.T) {
		if err := validateYAML("invalid", true); err == nil {
			t.Fatal("validateYAML() expected a root mapping error")
		}
	})
}
