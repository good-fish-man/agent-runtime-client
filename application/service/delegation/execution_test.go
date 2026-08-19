package delegation

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	delegationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	runtimerepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	delegationrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/delegation"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

type specialistRuntimeStub struct {
	calls        atomic.Int64
	withEvidence bool
	content      string
	last         *runtimeentity.RunInput
	mu           sync.Mutex
}

func (s *specialistRuntimeStub) Run(context.Context, *runtimeentity.RunInput) (*runtimeentity.Completion, error) {
	return nil, nil
}

func (s *specialistRuntimeStub) RunStream(_ context.Context, input *runtimeentity.RunInput, emit runtimerepo.StreamFunc) error {
	s.calls.Add(1)
	s.mu.Lock()
	s.last = input
	s.mu.Unlock()
	if err := emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeToolCall, ToolCall: &runtimeentity.ToolCallEvent{ID: "call-1", Tool: "internet_search"}}); err != nil {
		return err
	}
	if s.withEvidence {
		suffix := strings.TrimPrefix(contextString(input.Context, "task_id"), "task-")
		suffix = strings.NewReplacer("/", "-", " ", "-").Replace(suffix)
		if suffix == "" {
			suffix = "default"
		}
		if err := emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeToolResult, ToolResult: &runtimeentity.ToolResultEvent{ID: "call-1", Tool: "internet_search", Success: true, Output: map[string]any{"url": "https://example.com/source/" + suffix}}}); err != nil {
			return err
		}
	}
	content := s.content
	if content == "" {
		content = "Evidence-backed comparison"
	}
	return emit(&runtimeentity.StreamEvent{Type: runtimeentity.StreamTypeDone, Done: &runtimeentity.DoneEvent{
		Content: content, FinishReason: "stop", PromptTokens: 120, CompletionTokens: 40, TotalTokens: 160, FinishedAt: time.Now().UTC(),
	}})
}

func TestExecutionServiceFastPathHasNoRuntimeOrPersistenceOverhead(t *testing.T) {
	service, runtime, db := newExecutionFixture(t, true)
	latencies := make([]time.Duration, 0, 200)
	for index := 0; index < cap(latencies); index++ {
		started := time.Now()
		handled, err := service.MaybeRunStream(context.Background(), executionInput("hello"), nil)
		if err != nil || handled {
			t.Fatalf("fast path run %d handled=%v err=%v", index, handled, err)
		}
		latencies = append(latencies, time.Since(started))
	}
	if runtime.calls.Load() != 0 {
		t.Fatalf("fast path invoked specialist runtime %d times", runtime.calls.Load())
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p95 := latencies[(len(latencies)*95+99)/100-1]
	if p95 > 50*time.Millisecond {
		t.Fatalf("fast path p95 overhead = %v", p95)
	}
	var count int64
	if err := db.Model(&po.Run{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("fast path created runs: count=%d err=%v", count, err)
	}
}

func TestExecutionServicePersistsCompleteSingleSpecialistChain(t *testing.T) {
	service, runtime, db := newExecutionFixture(t, true)
	for index := 0; index < 20; index++ {
		handled, err := service.MaybeRunStream(context.Background(), executionInput("Compare Isaac Sim, Gazebo and AWSIM using current evidence and sources"), func(*runtimeentity.StreamEvent) error { return nil })
		if err != nil || !handled {
			t.Fatalf("run %d handled=%v err=%v", index, handled, err)
		}
	}
	if runtime.calls.Load() != 20 {
		t.Fatalf("specialist runtime calls = %d", runtime.calls.Load())
	}
	for name, model := range map[string]any{
		"proposal": &po.Proposal{}, "run": &po.Run{}, "manifest": &po.InvocationManifest{},
		"attempt": &po.Attempt{}, "turn": &po.DecisionTurn{}, "model invocation": &po.ModelInvocation{},
		"candidate": &po.CandidateResult{}, "verification": &po.VerificationResult{},
	} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 20 {
			t.Fatalf("%s count=%d err=%v", name, count, err)
		}
	}
	var verification po.VerificationResult
	if err := db.Order("verified_at DESC").First(&verification).Error; err != nil {
		t.Fatal(err)
	}
	if verification.Status != delegationentity.VerificationSatisfied || !strings.Contains(verification.EvidenceRefs, "example.com/source") {
		t.Fatalf("verification = %+v", verification)
	}
	runtime.mu.Lock()
	last := runtime.last
	runtime.mu.Unlock()
	if last == nil || len(last.Capabilities) != 1 || last.Capabilities[0].ID != "internet.search" {
		t.Fatalf("admitted runtime capabilities = %+v", last)
	}
	if len(last.SubAgents) != 0 || len(last.MCPs) != 0 || len(last.Messages) != 0 {
		t.Fatalf("specialist received unbounded execution inputs: %+v", last)
	}

	var linkedChains int64
	if err := db.Table("os_delegation_proposal AS proposal").
		Joins("JOIN os_subagent_run AS run ON run.owner_id = proposal.owner_id AND run.goal_id = proposal.goal_id AND run.task_step_id = proposal.task_step_id").
		Joins("JOIN os_invocation_manifest AS manifest ON manifest.run_id = run.run_id AND manifest.owner_id = run.owner_id").
		Joins("JOIN os_subagent_attempt AS attempt ON attempt.run_id = run.run_id AND attempt.invocation_manifest_id = manifest.manifest_id").
		Joins("JOIN os_decision_turn AS turn ON turn.attempt_id = attempt.attempt_id AND turn.owner_id = run.owner_id").
		Joins("JOIN os_model_invocation AS invocation ON invocation.turn_id = turn.turn_id AND invocation.owner_id = run.owner_id").
		Joins("JOIN os_candidate_result AS candidate ON candidate.run_id = run.run_id AND candidate.attempt_id = attempt.attempt_id").
		Joins("JOIN os_dso_verification_result AS verification ON verification.run_id = run.run_id AND verification.attempt_id = attempt.attempt_id").
		Count(&linkedChains).Error; err != nil || linkedChains != 20 {
		t.Fatalf("complete linked trace count=%d err=%v", linkedChains, err)
	}

	var candidates []po.CandidateResult
	if err := db.Find(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range candidates {
		var typed dso.TypedCandidateResult
		if err := json.Unmarshal([]byte(row.Content), &typed); err != nil {
			t.Fatalf("candidate %s is not typed JSON: %v", row.ResultID, err)
		}
		if err := typed.Validate(); err != nil {
			t.Fatalf("candidate %s failed schema validation: %v", row.ResultID, err)
		}
	}

	var manifest po.InvocationManifest
	var contextSlice po.ContextSlice
	if err := db.First(&manifest).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&contextSlice).Error; err != nil {
		t.Fatal(err)
	}
	persistedArtifacts := manifest.Content + contextSlice.Content
	for _, secret := range []string{"private-runtime-key", "sk-do-not-leak-123456"} {
		if strings.Contains(persistedArtifacts, secret) {
			t.Fatalf("secret %q leaked into immutable artifacts", secret)
		}
	}
}

func TestCandidateSelfReportedSuccessDoesNotSatisfyOutcomeWithoutEvidence(t *testing.T) {
	service, _, db := newExecutionFixture(t, false)
	service.runtime.(*specialistRuntimeStub).content = `SUCCESS: all requirements are verified`
	handled, err := service.MaybeRunStream(context.Background(), executionInput("Compare Isaac Sim, Gazebo and AWSIM using current evidence and sources"), nil)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	var verification po.VerificationResult
	if err := db.First(&verification).Error; err != nil {
		t.Fatal(err)
	}
	if verification.Status != delegationentity.VerificationUnknown {
		t.Fatalf("self-reported success changed external verification: %+v", verification)
	}
}

func newExecutionFixture(t *testing.T, evidence bool) (*ExecutionService, *specialistRuntimeStub, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&po.Proposal{}, &po.Decision{}, &po.DelegatedOutcome{}, &po.SubagentSpec{},
		&po.ContextSlice{}, &po.CapabilityView{}, &po.ActorBinding{}, &po.InvocationManifest{},
		&po.Run{}, &po.Attempt{}, &po.DecisionTurn{}, &po.ModelInvocation{}, &po.BudgetAccount{},
		&po.BudgetReservation{}, &po.ResourceLease{}, &po.CandidateResult{}, &po.VerificationResult{}, &po.Event{},
		&po.ParallelPlan{}, &po.ParallelNode{}, &po.ParallelAggregate{},
	); err != nil {
		t.Fatal(err)
	}
	store := delegationrepo.NewStore(data.New(db))
	orchestrator := NewOrchestrator(store, Config{InstanceID: "worker-test", ScanInterval: time.Hour, LeaseTTL: time.Minute}, nil)
	runtime := &specialistRuntimeStub{withEvidence: evidence}
	return NewExecutionService(orchestrator, runtime, nil), runtime, db
}

func executionInput(prompt string) *runtimeentity.RunInput {
	return &runtimeentity.RunInput{
		Prompt: prompt,
		Models: map[string]runtimeentity.ModelConfig{"default": {
			Provider: "openai", Name: "test-model", APIKey: "private-runtime-key", APIBase: "https://api.example.com/v1",
		}},
		Context: map[string]any{
			"user_id": "owner-1", "run_manifest_id": "parent-manifest-1", "locale": "en-US",
			"knowledge_context": map[string]any{"summary": "safe", "api_key": "sk-do-not-leak-123456"},
		},
		Capabilities: []runtimeentity.CapabilityConfig{{ID: "internet.search"}, {ID: "system.shell"}},
		Options:      &runtimeentity.RunOptions{Stream: true, TimeoutMs: 30000, MaxTotalTokens: 2000},
	}
}
