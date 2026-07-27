package model

import "testing"

func TestRequiresAPIKey(t *testing.T) {
	tests := []struct {
		provider string
		baseURL  string
		want     bool
	}{
		{provider: "OpenAI", baseURL: "https://api.openai.com/v1", want: true},
		{provider: "Ollama", baseURL: "http://example.com", want: false},
		{provider: "LM Studio", baseURL: "", want: false},
		{provider: "Custom", baseURL: "http://localhost:8000/v1", want: false},
		{provider: "Custom", baseURL: "http://192.168.1.20:8000/v1", want: false},
		{provider: "OpenAI", baseURL: "http://host.docker.internal:8000/v1", want: false},
		{provider: "Custom", baseURL: "http://local-model:8000/v1", want: false},
		{provider: "Xinference", baseURL: "", want: false},
	}
	for _, tt := range tests {
		if got := RequiresAPIKey(tt.provider, tt.baseURL); got != tt.want {
			t.Errorf("RequiresAPIKey(%q, %q) = %v, want %v", tt.provider, tt.baseURL, got, tt.want)
		}
	}
}
