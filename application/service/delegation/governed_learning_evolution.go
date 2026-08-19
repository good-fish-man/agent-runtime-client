package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
	log "github.com/good-fish-man/logx"
)

type GovernedEvolutionConfig struct {
	Enabled      bool
	ScanInterval time.Duration
	BatchSize    int
}

type GovernedEvolutionSnapshot struct {
	Enabled           bool      `json:"enabled"`
	Running           bool      `json:"running"`
	LastScanAt        time.Time `json:"last_scan_at,omitempty"`
	LastSuccessAt     time.Time `json:"last_success_at,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
	ScannedSources    int       `json:"scanned_sources"`
	CreatedCandidates int       `json:"created_candidates"`
}

type GovernedLearningEvolution struct {
	learning *GovernedLearningService
	source   repository.AdHocStore
	config   GovernedEvolutionConfig

	mu     sync.RWMutex
	status GovernedEvolutionSnapshot
	cancel context.CancelFunc
	done   chan struct{}
}

func NewGovernedLearningEvolution(learning *GovernedLearningService, source repository.AdHocStore, config GovernedEvolutionConfig) *GovernedLearningEvolution {
	if config.ScanInterval <= 0 {
		config.ScanInterval = 5 * time.Minute
	}
	if config.BatchSize <= 0 || config.BatchSize > 500 {
		config.BatchSize = 100
	}
	return &GovernedLearningEvolution{learning: learning, source: source, config: config, status: GovernedEvolutionSnapshot{Enabled: config.Enabled}}
}

func (o *GovernedLearningEvolution) Start(parent context.Context) {
	if o == nil || !o.config.Enabled || o.learning == nil || o.source == nil {
		return
	}
	o.mu.Lock()
	if o.cancel != nil {
		o.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	o.cancel, o.done = cancel, make(chan struct{})
	o.status.Running = true
	o.mu.Unlock()
	go o.loop(ctx)
}

func (o *GovernedLearningEvolution) Stop() {
	if o == nil {
		return
	}
	o.mu.Lock()
	cancel, done := o.cancel, o.done
	o.cancel = nil
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (o *GovernedLearningEvolution) Snapshot() GovernedEvolutionSnapshot {
	if o == nil {
		return GovernedEvolutionSnapshot{}
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.status
}

func (o *GovernedLearningEvolution) ScanOnce(ctx context.Context) (GovernedEvolutionSnapshot, error) {
	if o == nil || o.learning == nil || o.source == nil {
		return GovernedEvolutionSnapshot{}, fmt.Errorf("governed delegation evolution is not configured")
	}
	started := time.Now().UTC()
	sources, err := o.source.ListPendingProfileCandidates(ctx, o.config.BatchSize)
	if err != nil {
		o.recordScan(started, 0, 0, err)
		return o.Snapshot(), err
	}
	created := 0
	var scanErrors []error
	for _, source := range sources {
		count, scanErr := o.importProfileSource(ctx, source.OwnerID, source.CandidateID)
		if errors.Is(scanErr, ErrDelegationLearningDisabled) {
			continue
		}
		if scanErr != nil {
			scanErrors = append(scanErrors, log.WrapError(scanErr, "GovernedLearningEvolution.source["+source.CandidateID+"]"))
			continue
		}
		created += count
	}
	joined := errors.Join(scanErrors...)
	o.recordScan(started, len(sources), created, joined)
	return o.Snapshot(), joined
}

func (o *GovernedLearningEvolution) loop(ctx context.Context) {
	defer func() {
		o.mu.Lock()
		o.status.Running = false
		o.done = nil
		o.mu.Unlock()
	}()
	_, _ = o.ScanOnce(ctx)
	ticker := time.NewTicker(o.config.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := o.ScanOnce(ctx); err != nil {
				log.Warnf(ctx, "governed delegation evolution scan failed: %v", err)
			}
		}
	}
}

func (o *GovernedLearningEvolution) importProfileSource(ctx context.Context, ownerID, sourceCandidateID string) (int, error) {
	source, err := o.source.FindProfileCandidate(ctx, ownerID, sourceCandidateID)
	if err != nil || source == nil {
		if err == nil {
			err = fmt.Errorf("source specialist profile candidate not found")
		}
		return 0, err
	}
	var sourceCandidate dso.SpecialistProfileCandidate
	if err := json.Unmarshal([]byte(source.Content), &sourceCandidate); err != nil {
		return 0, err
	}
	if err := sourceCandidate.Validate(); err != nil {
		return 0, err
	}
	overlayRecord, _, err := o.source.FindAdHocOverlay(ctx, ownerID, sourceCandidate.SourceOverlayRef)
	if err != nil || overlayRecord == nil {
		if err == nil {
			err = fmt.Errorf("source ad-hoc overlay not found")
		}
		return 0, err
	}
	var overlay dso.AdHocSpecialistOverlay
	if err := json.Unmarshal([]byte(overlayRecord.Content), &overlay); err != nil {
		return 0, err
	}
	base, ok := reviewedSpecialistProfile("", overlay.BaseProfileRef)
	if !ok {
		return 0, fmt.Errorf("reviewed base profile %s not found", overlay.BaseProfileRef)
	}
	experienceRefs := make([]string, 0, len(sourceCandidate.SuccessfulRunRefs))
	for _, runRef := range sourceCandidate.SuccessfulRunRefs {
		experienceRefs = append(experienceRefs, "delegation-experience://"+runRef)
	}
	profileArtifact := &dso.SpecialistProfileArtifact{
		ArtifactID: "learned-" + sourceCandidate.CandidateID, Version: "v1", BaseProfileRef: overlay.BaseProfileRef,
		Role: overlay.RoleDescription, Capabilities: append([]string(nil), overlay.RequestedCapabilities...),
		ContextScope: overlay.RequestedContextScope, RiskCeiling: base.RiskCeiling,
		PromptArtifactRef: base.PromptArtifactRef, OutputSchemaRef: base.OutputSchemaRef,
	}
	profileID := "dso-profile-" + sourceCandidate.CandidateID
	created := 0
	if existing, findErr := o.learning.store.FindLearningCandidate(ctx, ownerID, profileID); findErr != nil {
		return 0, findErr
	} else if existing == nil {
		if _, err := o.learning.ProposeCandidate(ctx, LearningCandidateInput{
			CandidateID: profileID, OwnerID: ownerID, Kind: dso.LearningCandidateSpecialistProfile,
			SourceExperienceRefs: experienceRefs, SourceRunRefs: sourceCandidate.SuccessfulRunRefs,
			ProfileArtifact: profileArtifact, CreatedAt: sourceCandidate.CreatedAt,
		}); err != nil {
			return created, err
		}
		created++
	}
	policyID := "dso-policy-" + sourceCandidate.CandidateID
	if existing, findErr := o.learning.store.FindLearningCandidate(ctx, ownerID, policyID); findErr != nil {
		return created, findErr
	} else if existing == nil {
		taskClass := "general"
		for _, capability := range overlay.RequestedCapabilities {
			if strings.HasPrefix(capability, "internet.") || strings.HasPrefix(capability, "browser.") || strings.HasPrefix(capability, "github.") {
				taskClass = "research"
				break
			}
		}
		policy := &dso.DelegationPolicyArtifact{
			ArtifactID: "learned-policy-" + sourceCandidate.CandidateID, Version: "v1", DefaultFallbackRef: dso.RulePolicyFallbackRef,
			Rules: []dso.DelegationPolicyRule{{
				RuleID: "learned-" + taskClass, TaskClasses: []string{taskClass},
				RequiredCapabilities:  append([]string(nil), overlay.RequestedCapabilities...),
				RecommendedProfileRef: "specialist-profile://" + profileArtifact.ArtifactID + "/" + profileArtifact.Version,
				RiskCeiling:           "low", MinimumComplexity: 0.5, MaximumParallelism: 3, BudgetMultiplier: 1,
			}},
		}
		if _, err := o.learning.ProposeCandidate(ctx, LearningCandidateInput{
			CandidateID: policyID, OwnerID: ownerID, Kind: dso.LearningCandidateDelegationPolicy,
			SourceExperienceRefs: experienceRefs, SourceRunRefs: sourceCandidate.SuccessfulRunRefs,
			PolicyArtifact: policy, CreatedAt: sourceCandidate.CreatedAt,
		}); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

func (o *GovernedLearningEvolution) recordScan(at time.Time, scanned, created int, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.status.LastScanAt, o.status.ScannedSources, o.status.CreatedCandidates = at, scanned, created
	if err != nil {
		o.status.LastError = err.Error()
		return
	}
	o.status.LastError = ""
	o.status.LastSuccessAt = time.Now().UTC()
}
