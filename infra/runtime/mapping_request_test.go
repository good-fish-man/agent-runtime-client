package runtime

import (
	"testing"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

func TestToSubAgentsMapsExecutionLimits(t *testing.T) {
	items := toSubAgents([]entity.SubAgentConfig{{
		ID: "reviewer", Name: "Reviewer", MaxIterations: 7, TimeoutMs: 45000,
	}})
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].GetId() != "reviewer" || items[0].GetMaxIterations() != 7 || items[0].GetTimeoutMs() != 45000 {
		t.Fatalf("mapped sub-agent = %+v", items[0])
	}
}
