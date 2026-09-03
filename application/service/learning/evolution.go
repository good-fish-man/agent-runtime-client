package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	experienceentity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
	log "github.com/good-fish-man/logx"
)

const evolutionOrigin = "EVOLUTION_ORCHESTRATOR"

type EvolutionConfig struct {
	Enabled                 bool
	ScanInterval            time.Duration
	OwnerBatchSize          int
	ExperienceLimit         int
	MaxCandidatesPerScan    int
	MinimumNovelExperiences int
}

type EvolutionExperienceStore interface {
	GetPreference(context.Context, string) (*experienceentity.Preference, error)
	ListReadyOwners(context.Context, string, int) ([]string, error)
	List(context.Context, string, experienceentity.ListFilter) ([]experienceentity.Experience, int64, error)
}

type EvolutionSnapshot struct {
	Enabled            bool      `json:"enabled"`
	Running            bool      `json:"running"`
	AISynthesisEnabled bool      `json:"ai_synthesis_enabled"`
	AISynthesisModel   string    `json:"ai_synthesis_model,omitempty"`
	Scans              int64     `json:"scans"`
	OwnersScanned      int64     `json:"owners_scanned"`
	PatternsDiscovered int64     `json:"patterns_discovered"`
	CandidatesProposed int64     `json:"candidates_proposed"`
	CandidatesSkipped  int64     `json:"candidates_skipped"`
	LastScanAt         time.Time `json:"last_scan_at,omitempty"`
	LastCompletedAt    time.Time `json:"last_completed_at,omitempty"`
	LastError          string    `json:"last_error,omitempty"`
}

type EvolutionScanResult struct {
	OwnersScanned      int      `json:"owners_scanned"`
	PatternsDiscovered int      `json:"patterns_discovered"`
	CandidatesProposed int      `json:"candidates_proposed"`
	CandidatesSkipped  int      `json:"candidates_skipped"`
	CandidateIDs       []string `json:"candidate_ids,omitempty"`
}

type EvolutionOrchestrator struct {
	learning    *Service
	experiences EvolutionExperienceStore
	config      EvolutionConfig

	workerMu sync.Mutex
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	scanMu   sync.Mutex
	statusMu sync.RWMutex
	status   EvolutionSnapshot
}

func NewEvolutionOrchestrator(learning *Service, experiences EvolutionExperienceStore, config EvolutionConfig) *EvolutionOrchestrator {
	config = normalizeEvolutionConfig(config)
	status := EvolutionSnapshot{Enabled: config.Enabled}
	if learning != nil && learning.synthesizer != nil {
		status.AISynthesisEnabled = true
		status.AISynthesisModel = learning.synthesizer.ModelName()
	}
	return &EvolutionOrchestrator{learning: learning, experiences: experiences, config: config, status: status}
}

func (o *EvolutionOrchestrator) Start(parent context.Context) {
	if o == nil || !o.config.Enabled || o.learning == nil || o.experiences == nil {
		return
	}
	o.workerMu.Lock()
	defer o.workerMu.Unlock()
	if o.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	o.cancel = cancel
	o.wg.Add(1)
	log.Go(ctx, func(ctx context.Context) {
		defer o.wg.Done()
		o.loop(ctx)
	})
}

func (o *EvolutionOrchestrator) Stop() {
	if o == nil {
		return
	}
	o.workerMu.Lock()
	cancel := o.cancel
	o.cancel = nil
	o.workerMu.Unlock()
	if cancel != nil {
		cancel()
		o.wg.Wait()
	}
}

func (o *EvolutionOrchestrator) Snapshot() EvolutionSnapshot {
	if o == nil {
		return EvolutionSnapshot{}
	}
	o.statusMu.RLock()
	defer o.statusMu.RUnlock()
	return o.status
}

func (o *EvolutionOrchestrator) ScanOnce(ctx context.Context) (EvolutionScanResult, error) {
	return o.scan(ctx, "")
}

func (o *EvolutionOrchestrator) ScanOwner(ctx context.Context, ownerID string) (EvolutionScanResult, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return EvolutionScanResult{}, fmt.Errorf("owner_id is required")
	}
	return o.scan(ctx, ownerID)
}

func (o *EvolutionOrchestrator) loop(ctx context.Context) {
	_, _ = o.ScanOnce(ctx)
	ticker := time.NewTicker(o.config.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := o.ScanOnce(ctx); err != nil && ctx.Err() == nil {
				log.Warnf(ctx, "evolution scan failed: %v", err)
			}
		}
	}
}

func (o *EvolutionOrchestrator) scan(ctx context.Context, onlyOwner string) (result EvolutionScanResult, resultErr error) {
	if o == nil || o.learning == nil || o.experiences == nil {
		return result, fmt.Errorf("evolution orchestrator is not configured")
	}
	if !o.config.Enabled {
		return result, fmt.Errorf("evolution orchestrator is disabled")
	}
	o.scanMu.Lock()
	defer o.scanMu.Unlock()
	started := time.Now().UTC()
	o.updateStatus(func(status *EvolutionSnapshot) {
		status.Running = true
		status.LastScanAt = started
		status.LastError = ""
	})
	defer func() {
		completed := time.Now().UTC()
		o.updateStatus(func(status *EvolutionSnapshot) {
			status.Running = false
			status.Scans++
			status.OwnersScanned += int64(result.OwnersScanned)
			status.PatternsDiscovered += int64(result.PatternsDiscovered)
			status.CandidatesProposed += int64(result.CandidatesProposed)
			status.CandidatesSkipped += int64(result.CandidatesSkipped)
			status.LastCompletedAt = completed
			if resultErr != nil {
				status.LastError = resultErr.Error()
			}
		})
	}()

	owners := []string(nil)
	if onlyOwner != "" {
		owners = []string{onlyOwner}
	} else {
		after := ""
		for {
			page, err := o.experiences.ListReadyOwners(ctx, after, o.config.OwnerBatchSize)
			if err != nil {
				return result, log.WrapError(err, "EvolutionOrchestrator.Scan.owners")
			}
			owners = append(owners, page...)
			if len(page) < o.config.OwnerBatchSize {
				break
			}
			after = page[len(page)-1]
		}
	}
	ownerErrors := make([]error, 0)
	for _, ownerID := range owners {
		result.OwnersScanned++
		ownerResult, err := o.scanOwner(ctx, ownerID, o.config.MaxCandidatesPerScan-result.CandidatesProposed)
		if err != nil {
			wrapped := log.WrapError(err, "EvolutionOrchestrator.Scan.owner["+ownerID+"]")
			ownerErrors = append(ownerErrors, wrapped)
			log.Warnw(ctx, "evolution owner scan failed", "owner_id", ownerID, "error", wrapped)
			continue
		}
		result.PatternsDiscovered += ownerResult.PatternsDiscovered
		result.CandidatesProposed += ownerResult.CandidatesProposed
		result.CandidatesSkipped += ownerResult.CandidatesSkipped
		result.CandidateIDs = append(result.CandidateIDs, ownerResult.CandidateIDs...)
		if result.CandidatesProposed >= o.config.MaxCandidatesPerScan {
			break
		}
	}
	log.Infow(ctx, "evolution scan completed",
		"owners", result.OwnersScanned, "patterns", result.PatternsDiscovered,
		"proposed", result.CandidatesProposed, "skipped", result.CandidatesSkipped,
		"latency_ms", time.Since(started).Milliseconds(),
	)
	if len(ownerErrors) > 0 {
		return result, log.WrapError(errors.Join(ownerErrors...), "EvolutionOrchestrator.Scan.ownerFailures")
	}
	return result, nil
}

func (o *EvolutionOrchestrator) scanOwner(ctx context.Context, ownerID string, remaining int) (EvolutionScanResult, error) {
	result := EvolutionScanResult{}
	if remaining <= 0 {
		return result, nil
	}
	preference, err := o.experiences.GetPreference(ctx, ownerID)
	if err != nil {
		return result, err
	}
	if preference == nil || !preference.LearningEnabled {
		return result, nil
	}
	experiences, err := o.loadReadyExperiences(ctx, ownerID)
	if err != nil {
		return result, err
	}
	patterns := discoverPatternEvidence(experiences)
	result.PatternsDiscovered = len(patterns)
	previous, _, err := o.learning.ListCandidates(ctx, ownerID, entity.CandidateFilter{Kind: entity.CandidateSkill, Limit: 200})
	if err != nil {
		return result, err
	}
	for _, proposal := range patterns {
		if result.CandidatesProposed >= remaining {
			break
		}
		version, allowed := nextAutomaticVersion(proposal.Pattern, proposal.Evidence, previous, o.config.MinimumNovelExperiences)
		if !allowed {
			result.CandidatesSkipped++
			continue
		}
		evidenceIDs := experienceIDs(proposal.Evidence)
		digest := evidenceDigest(ownerID, proposal.Pattern, evidenceIDs)
		candidateID := "evo-" + digest[:32]
		if existing, err := o.learning.store.FindCandidate(ctx, ownerID, candidateID); err != nil {
			return result, err
		} else if existing != nil {
			result.CandidatesSkipped++
			continue
		}
		request := GenerateCandidateRequest{
			Kind: entity.CandidateSkill, ID: "learned." + normalizeID(strings.ReplaceAll(proposal.Pattern, "|", ".")),
			Version: version, Description: "Reviewed reusable action plan discovered from independent runtime outcomes.",
			ExperienceIDs: evidenceIDs, Visibility: entity.VisibilityPrivate, MinimumScore: defaultMinimumScore,
		}
		candidate, err := o.learning.generateCandidate(ctx, ownerID, request, generationOptions{CandidateID: candidateID, Origin: evolutionOrigin, EvidenceDigest: digest})
		if err != nil {
			// A deterministic ID makes concurrent scans idempotent across service replicas.
			if existing, findErr := o.learning.store.FindCandidate(ctx, ownerID, candidateID); findErr == nil && existing != nil {
				result.CandidatesSkipped++
				continue
			}
			return result, err
		}
		previous = append(previous, *candidate)
		result.CandidatesProposed++
		result.CandidateIDs = append(result.CandidateIDs, candidate.CandidateID)
		log.Infow(ctx, "evolution candidate proposed", "owner_id", ownerID, "candidate_id", candidate.CandidateID, "pattern", proposal.Pattern, "version", version, "status", candidate.Status)
	}
	return result, nil
}

func (o *EvolutionOrchestrator) loadReadyExperiences(ctx context.Context, ownerID string) ([]experienceentity.Experience, error) {
	result := make([]experienceentity.Experience, 0, o.config.ExperienceLimit)
	for offset := 0; offset < o.config.ExperienceLimit; {
		limit := 200
		if remaining := o.config.ExperienceLimit - offset; remaining < limit {
			limit = remaining
		}
		page, total, err := o.experiences.List(ctx, ownerID, experienceentity.ListFilter{Status: experienceentity.StatusReady, Limit: limit, Offset: offset})
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		offset += len(page)
		if len(page) == 0 || int64(offset) >= total {
			break
		}
	}
	return result, nil
}

type patternEvidence struct {
	Pattern  string
	Evidence []experienceentity.Experience
}

func discoverPatternEvidence(items []experienceentity.Experience) []patternEvidence {
	successes := make(map[string][]experienceentity.Experience)
	failures := make(map[string][]experienceentity.Experience)
	for _, item := range items {
		if len(item.ActionRefs) == 0 {
			continue
		}
		pattern := actionPattern(item.ActionRefs)
		switch item.Outcome {
		case experienceentity.OutcomeSucceeded:
			successes[pattern] = append(successes[pattern], item)
		case experienceentity.OutcomeFailed:
			failures[pattern] = append(failures[pattern], item)
		}
	}
	result := make([]patternEvidence, 0)
	for pattern, successful := range successes {
		if len(successful) < minimumSuccessCount || len(failures[pattern]) == 0 {
			continue
		}
		selected := append([]experienceentity.Experience(nil), successful...)
		selected = append(selected, failures[pattern]...)
		if len(selected) < minimumEvidenceCount {
			continue
		}
		sort.Slice(selected, func(i, j int) bool {
			if selected[i].CreatedAt.Equal(selected[j].CreatedAt) {
				return selected[i].ExperienceID < selected[j].ExperienceID
			}
			return selected[i].CreatedAt.After(selected[j].CreatedAt)
		})
		if len(selected) > maximumEvidenceCount {
			selected = selected[:maximumEvidenceCount]
		}
		if validateIndependentEvidence(selected) == nil {
			result = append(result, patternEvidence{Pattern: pattern, Evidence: selected})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i].Evidence) == len(result[j].Evidence) {
			return result[i].Pattern < result[j].Pattern
		}
		return len(result[i].Evidence) > len(result[j].Evidence)
	})
	return result
}

func nextAutomaticVersion(pattern string, evidence []experienceentity.Experience, candidates []entity.Candidate, minimumNovel int) (string, bool) {
	latestPatch := -1
	latestCreated := time.Time{}
	latestEvidence := map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate.Evidence.Pattern != pattern || candidate.Skill == nil || candidate.Skill.Metadata["origin"] != evolutionOrigin {
			continue
		}
		switch candidate.Status {
		case entity.LifecycleDraft, entity.LifecycleValidating, entity.LifecycleEvaluating, entity.LifecycleReviewRequired:
			return "", false
		}
		if patch, ok := semanticPatch(candidate.Skill.Version); ok && patch > latestPatch {
			latestPatch = patch
		}
		if candidate.CreatedAt.After(latestCreated) {
			latestCreated = candidate.CreatedAt
			latestEvidence = make(map[string]struct{}, len(candidate.Evidence.ExperienceIDs))
			for _, id := range candidate.Evidence.ExperienceIDs {
				latestEvidence[id] = struct{}{}
			}
		}
	}
	if latestPatch >= 0 {
		novel := 0
		for _, item := range evidence {
			if _, exists := latestEvidence[item.ExperienceID]; !exists {
				novel++
			}
		}
		if novel < minimumNovel {
			return "", false
		}
	}
	return fmt.Sprintf("0.1.%d", latestPatch+1), true
}

func semanticPatch(version string) (int, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")
	if len(parts) != 3 || parts[0] != "0" || parts[1] != "1" {
		return 0, false
	}
	value, err := strconv.Atoi(parts[2])
	return value, err == nil && value >= 0
}

func experienceIDs(values []experienceentity.Experience) []string {
	ids := make([]string, 0, len(values))
	for _, value := range values {
		ids = append(ids, value.ExperienceID)
	}
	sort.Strings(ids)
	return ids
}

func evidenceDigest(ownerID, pattern string, ids []string) string {
	digest := sha256.Sum256([]byte(ownerID + "\x00" + pattern + "\x00" + strings.Join(ids, "\x00")))
	return hex.EncodeToString(digest[:])
}

func normalizeEvolutionConfig(config EvolutionConfig) EvolutionConfig {
	if config.ScanInterval <= 0 {
		config.ScanInterval = time.Minute
	}
	if config.OwnerBatchSize <= 0 || config.OwnerBatchSize > 200 {
		config.OwnerBatchSize = 100
	}
	if config.ExperienceLimit <= 0 || config.ExperienceLimit > 2000 {
		config.ExperienceLimit = 1000
	}
	if config.MaxCandidatesPerScan <= 0 || config.MaxCandidatesPerScan > 100 {
		config.MaxCandidatesPerScan = 10
	}
	if config.MinimumNovelExperiences <= 0 {
		config.MinimumNovelExperiences = 2
	}
	return config
}

func (o *EvolutionOrchestrator) updateStatus(update func(*EvolutionSnapshot)) {
	o.statusMu.Lock()
	defer o.statusMu.Unlock()
	update(&o.status)
}
