package delegation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	delegationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	delegationrepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
)

func TestGovernedBrowserGoldenPathPersistsUnifiedChainFiftyTimes(t *testing.T) {
	store := newFakeActionStore()
	service := NewGovernedActionService(store, "instance-1")
	clock := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return clock }
	for iteration := 0; iteration < 50; iteration++ {
		action := validGovernedBrowserAction(fmt.Sprintf("action-%02d", iteration), clock.Add(time.Duration(iteration)*time.Millisecond))
		observation, err := service.Execute(context.Background(), validGovernedActionInput(action, fixedResourceReader("page-v1")), ActionDispatcherFunc(func(_ context.Context, action controlentity.Action) (*controlentity.Observation, error) {
			return &controlentity.Observation{ObservationID: "observation-" + action.ActionID, Status: controlentity.ObservationSucceeded, State: map[string]any{"resource_version": "page-v2", "playback": map[string]any{"playing": true}}}, nil
		}))
		if err != nil || observation == nil || observation.Status != controlentity.ObservationSucceeded {
			t.Fatalf("golden run %d failed: observation=%#v err=%v", iteration, observation, err)
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.chains) != 50 || len(store.completions) != 50 || store.leaseCount != 50 {
		t.Fatalf("incomplete governed chain: chains=%d completions=%d leases=%d", len(store.chains), len(store.completions), store.leaseCount)
	}
	for _, chain := range store.chains {
		if chain.Plan.PlanCandidateID == "" || chain.Policy.PolicyDecisionID == "" || chain.PlanRun.PlanRunID == "" || chain.Attempt.ActionAttemptID == "" {
			t.Fatalf("unified chain is incomplete: %#v", chain)
		}
	}
}

func TestGovernedActionCriticalRecheckBlocksPageDrift(t *testing.T) {
	store := newFakeActionStore()
	service := NewGovernedActionService(store, "instance-1")
	var reads atomic.Int32
	reader := func(context.Context) (delegationentity.ResourceSnapshot, error) {
		version := "page-v1"
		if reads.Add(1) > 1 {
			version = "page-v2"
		}
		return browserResource(version), nil
	}
	var dispatched atomic.Int32
	_, err := service.Execute(context.Background(), validGovernedActionInput(validGovernedBrowserAction("action-stale", time.Now()), reader), ActionDispatcherFunc(func(context.Context, controlentity.Action) (*controlentity.Observation, error) {
		dispatched.Add(1)
		return nil, nil
	}))
	if !errors.Is(err, delegationrepo.ErrResourceStale) {
		t.Fatalf("page drift did not return stale resource: %v", err)
	}
	if dispatched.Load() != 0 {
		t.Fatal("stale action reached the device dispatcher")
	}
	if got := store.lastCompletion().AttemptStatus; got != delegationentity.ActionFailed {
		t.Fatalf("stale action status=%q", got)
	}
}

func TestGovernedActionCancellationCreatesNoDeviceAction(t *testing.T) {
	store := newFakeActionStore()
	service := NewGovernedActionService(store, "instance-1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var dispatched atomic.Int32
	_, err := service.Execute(ctx, validGovernedActionInput(validGovernedBrowserAction("action-cancel", time.Now()), fixedResourceReader("page-v1")), ActionDispatcherFunc(func(context.Context, controlentity.Action) (*controlentity.Observation, error) {
		dispatched.Add(1)
		return nil, nil
	}))
	if !errors.Is(err, context.Canceled) || dispatched.Load() != 0 {
		t.Fatalf("cancelled action was not fenced: dispatched=%d err=%v", dispatched.Load(), err)
	}
	if got := store.lastCompletion().AttemptStatus; got != delegationentity.ActionCancelled {
		t.Fatalf("cancelled action status=%q", got)
	}
}

func TestGovernedActionLostObservationIsUnknownNotSuccess(t *testing.T) {
	store := newFakeActionStore()
	service := NewGovernedActionService(store, "instance-1")
	lost := errors.New("device disconnected after dispatch; observation lost")
	_, err := service.Execute(context.Background(), validGovernedActionInput(validGovernedBrowserAction("action-lost", time.Now()), fixedResourceReader("page-v1")), ActionDispatcherFunc(func(context.Context, controlentity.Action) (*controlentity.Observation, error) {
		return nil, lost
	}))
	if !errors.Is(err, lost) {
		t.Fatalf("lost observation origin was not retained: %v", err)
	}
	completion := store.lastCompletion()
	if completion.AttemptStatus != delegationentity.ActionUnknownOutcome || completion.PlanStatus != delegationentity.PlanRunUnknown {
		t.Fatalf("lost observation was falsely finalized: %#v", completion)
	}
}

func TestGovernedActionDeniedPolicyNeverReachesDevice(t *testing.T) {
	store := newFakeActionStore()
	service := NewGovernedActionService(store, "instance-1")
	action := validGovernedBrowserAction("action-denied", time.Now())
	action.Policy.Decision = controlentity.Block
	var dispatched atomic.Int32
	_, err := service.Execute(context.Background(), validGovernedActionInput(action, fixedResourceReader("page-v1")), ActionDispatcherFunc(func(context.Context, controlentity.Action) (*controlentity.Observation, error) {
		dispatched.Add(1)
		return nil, nil
	}))
	if !errors.Is(err, ErrActionPolicyDenied) || dispatched.Load() != 0 {
		t.Fatalf("denied action crossed policy boundary: dispatched=%d err=%v", dispatched.Load(), err)
	}
	if got := store.lastCompletion().AttemptStatus; got != delegationentity.ActionPolicyDenied {
		t.Fatalf("denied action status=%q", got)
	}
}

func TestGovernedActionPackageDoesNotBypassDispatcherBoundary(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), "athena-launcher") {
			t.Fatalf("%s bypasses the ActionDispatcher boundary", entry.Name())
		}
	}
}

func validGovernedActionInput(action controlentity.Action, reader func(context.Context) (delegationentity.ResourceSnapshot, error)) GovernedActionInput {
	return GovernedActionInput{
		OwnerID: "owner-1", GoalID: "goal-1", TaskStepID: "task-1", OutcomeID: "outcome-1",
		CapabilitySnapshotID: "capability-view-1", EnvironmentRef: "desktop://device-1",
		ActorDeviceID: "device-1", Action: action, ReadResource: reader,
	}
}

func validGovernedBrowserAction(id string, issuedAt time.Time) controlentity.Action {
	return controlentity.Action{
		ActionID: id, DecisionID: "decision-" + id, Capability: "browser.action", Operation: "play",
		DeviceID: "device-1", IssuedAt: issuedAt, Deadline: issuedAt.Add(time.Minute),
		Arguments: map[string]any{"ref": "@e2"}, Policy: controlentity.Policy{Decision: controlentity.Allow},
		ExpectedObservation: controlentity.ExpectedObservation{Kind: "media.playback_state"},
	}
}

func fixedResourceReader(version string) func(context.Context) (delegationentity.ResourceSnapshot, error) {
	return func(context.Context) (delegationentity.ResourceSnapshot, error) { return browserResource(version), nil }
}

func browserResource(version string) delegationentity.ResourceSnapshot {
	return delegationentity.ResourceSnapshot{ResourceRef: "browser://session/session-1/tab/tab-1", ResourceVersion: version, SessionID: "session-1", TabID: "tab-1", TaskRevision: 3}
}

type fakeActionStore struct {
	mu          sync.Mutex
	chains      []delegationentity.ActionChain
	completions []delegationentity.ActionCompletion
	leaseCount  int
}

func newFakeActionStore() *fakeActionStore { return &fakeActionStore{} }

func (s *fakeActionStore) CreateActionChain(_ context.Context, value delegationentity.ActionChain) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chains = append(s.chains, value)
	return nil
}

func (s *fakeActionStore) AcquireActionLease(_ context.Context, _ delegationentity.ResourceLease, expected, current string, _ time.Time, _ delegationentity.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected != current {
		return delegationrepo.ErrResourceStale
	}
	s.leaseCount++
	return nil
}

func (s *fakeActionStore) CompleteActionChain(_ context.Context, value delegationentity.ActionCompletion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completions = append(s.completions, value)
	return nil
}

func (s *fakeActionStore) FindActionAttempt(context.Context, string, string) (*delegationentity.GovernedActionAttemptRecord, error) {
	return nil, nil
}

func (s *fakeActionStore) lastCompletion() delegationentity.ActionCompletion {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.completions) == 0 {
		return delegationentity.ActionCompletion{}
	}
	return s.completions[len(s.completions)-1]
}
