package model

import (
	"net"
	"net/url"
	"strings"
)

// RequiresAPIKey reports whether a model endpoint should require credentials.
// Local inference providers and loopback/private-network endpoints may run
// without authentication, while public endpoints require a Key binding.
func RequiresAPIKey(provider, baseURL string) bool {
	normalizedProvider := strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(provider))
	switch normalizedProvider {
	case "ollama", "lmstudio", "localai", "vllm", "llamacpp", "xinference", "tgi", "textgenerationinference", "llamafile", "textgenerationwebui", "diffusers", "comfyui":
		return false
	}

	host := endpointHost(baseURL)
	if host == "" {
		return true
	}
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" || lowerHost == "host.docker.internal" || strings.HasSuffix(lowerHost, ".localhost") || strings.HasSuffix(lowerHost, ".local") {
		return false
	}
	if !strings.Contains(lowerHost, ".") && !strings.Contains(lowerHost, ":") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return false
	}
	return true
}

func endpointHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
