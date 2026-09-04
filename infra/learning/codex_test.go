package learning

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	learningsvc "github.com/good-fish-man/agent-runtime-client/application/service/learning"
)

func TestCodexSynthesizerUsesResponsesStructuredOutput(t *testing.T) {
	var captured responsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" || request.Method != http.MethodPost {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		proposal := learningsvc.CandidateSynthesisProposal{
			Description: "Codex synthesized navigation", Summary: "Bounded recovery for the observed failure",
			Steps: []learningsvc.CandidateSynthesisStep{{
				ID: "step-1", Capability: "browser.navigate", Operation: "navigate", DependsOn: []string{},
			}},
			RecoveryPaths: []learningsvc.CandidateSynthesisRecovery{{
				On: "VERIFICATION_FAILED", StepIDs: []string{"step-1"}, MaxAttempts: 1,
			}},
			VerificationRules: []learningsvc.CandidateSynthesisRule{{
				Field: "task.status", Operator: "equals", Expected: "COMPLETED", EvidenceRequired: true,
			}},
			FallbackOrder: []string{}, RetryBudget: learningsvc.CandidateSynthesisRetryBudget{},
		}
		text, _ := json.Marshal(proposal)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"id": "resp_123", "status": "completed",
			"output": []any{map[string]any{
				"type":    "message",
				"content": []any{map[string]any{"type": "output_text", "text": string(text)}},
			}},
		})
	}))
	defer server.Close()

	synthesizer, err := NewCodexSynthesizer(CodexConfig{
		Model: "gpt-5.6", APIKey: "test-key", APIBase: server.URL + "/v1",
		ReasoningEffort: "max", MaxOutputTokens: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := learningsvc.CandidateSynthesisInput{
		CandidateID: "candidate-1", SafetyID: "athena-hashed-owner", Kind: "SKILL",
		Pattern: "browser.navigate.navigate", DraftDescription: "Bounded draft",
		Actions: []learningsvc.CandidateSynthesisAction{{
			ID: "step-1", Capability: "browser.navigate", Operation: "navigate", DependsOn: []string{},
		}},
		Evidence: []learningsvc.CandidateSynthesisEvidence{{
			Outcome: "FAILED", FailureClass: "VERIFICATION_FAILED", Context: "context-1",
		}},
		Capabilities: []learningsvc.CandidateSynthesisCapability{{
			ID: "browser.navigate", Risk: "MEDIUM", Operations: []string{"navigate"},
		}},
		FallbackOrder: []string{},
	}
	result, err := synthesizer.Synthesize(context.Background(), input)
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if result.Model != "gpt-5.6" || result.ResponseID != "resp_123" || len(result.InputDigest) != 64 {
		t.Fatalf("result = %+v", result)
	}
	if result.Proposal.Description != "Codex synthesized navigation" {
		t.Fatalf("proposal = %+v", result.Proposal)
	}
	if captured.Model != "gpt-5.6" || captured.Store || captured.Reasoning.Effort != "max" || captured.MaxOutputTokens != 2048 {
		t.Fatalf("request = %+v", captured)
	}
	if captured.SafetyIdentifier != "athena-hashed-owner" || captured.Text.Format.Type != "json_schema" || !captured.Text.Format.Strict {
		t.Fatalf("request safety/schema = %+v", captured)
	}
	if strings.Contains(captured.Input, "athena-hashed-owner") || strings.Contains(captured.Input, "test-key") {
		t.Fatalf("private configuration reached model input: %s", captured.Input)
	}
}

func TestCodexSynthesizerRejectsUnsafeConfiguration(t *testing.T) {
	for name, config := range map[string]CodexConfig{
		"missing key":   {Model: "gpt-5.6", APIKey: ""},
		"unsupported":   {Model: "gpt-4o", APIKey: "test-key"},
		"insecure base": {Model: "gpt-5.3-codex", APIKey: "test-key", APIBase: "http://api.example.test/v1"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCodexSynthesizer(config); err == nil {
				t.Fatalf("NewCodexSynthesizer(%+v) succeeded", config)
			}
		})
	}
}

func TestCodexSynthesizerSupportsGPT56FamilyAndReasoningEfforts(t *testing.T) {
	for _, model := range []string{"gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.3-codex"} {
		for _, effort := range []string{"none", "low", "medium", "high", "xhigh", "max"} {
			if _, err := NewCodexSynthesizer(CodexConfig{Model: model, APIKey: "test-key", ReasoningEffort: effort}); err != nil {
				t.Fatalf("model %q effort %q: %v", model, effort, err)
			}
		}
	}
}

func TestCodexSynthesizerSurfacesAPIErrorWithoutResponseParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"message":"rate limit exceeded"}}`))
	}))
	defer server.Close()
	synthesizer, err := NewCodexSynthesizer(CodexConfig{
		Model: "gpt-5.3-codex", APIKey: "test-key", APIBase: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = synthesizer.Synthesize(context.Background(), learningsvc.CandidateSynthesisInput{
		CandidateID: "candidate-1", SafetyID: "athena-owner", Kind: "SKILL",
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") || !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Fatalf("Synthesize() error = %v", err)
	}
}
