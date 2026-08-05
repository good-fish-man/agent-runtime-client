package runtime

import (
	"testing"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

func TestToSubAgentsMapsExecutionLimits(t *testing.T) {
	items := toSubAgents([]entity.SubAgentConfig{{
		ID: "reviewer", Name: "Reviewer", MaxIterations: 7, TimeoutMs: 45000,
		Capabilities: []entity.CapabilityConfig{{ID: "filesystem.read"}},
	}})
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].GetId() != "reviewer" || items[0].GetMaxIterations() != 7 || items[0].GetTimeoutMs() != 45000 {
		t.Fatalf("mapped sub-agent = %+v", items[0])
	}
	if len(items[0].GetCapabilities()) != 1 || items[0].GetCapabilities()[0].GetId() != "filesystem.read" {
		t.Fatalf("sub-agent capabilities were not mapped: %+v", items[0].GetCapabilities())
	}
}

func TestToRunRequestMapsCapabilities(t *testing.T) {
	request := toRunRequest(&entity.RunInput{Capabilities: []entity.CapabilityConfig{{
		ID: "internet.search", Config: map[string]any{"provider": "public"},
	}}})
	if len(request.GetCapabilities()) != 1 || request.GetCapabilities()[0].GetId() != "internet.search" {
		t.Fatalf("capabilities were not mapped: %+v", request.GetCapabilities())
	}
	if got := request.GetCapabilities()[0].GetConfig().AsMap()["provider"]; got != "public" {
		t.Fatalf("capability config = %v", got)
	}
}
