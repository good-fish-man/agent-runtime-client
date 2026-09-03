package delegationlearning

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	delegationsvc "github.com/good-fish-man/agent-runtime-client/application/service/delegation"
	delegationentity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	delegationrepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

type Handler struct {
	service   *delegationsvc.GovernedLearningService
	evolution *delegationsvc.GovernedLearningEvolution
}

func NewHandler(service *delegationsvc.GovernedLearningService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) WithEvolution(evolution *delegationsvc.GovernedLearningEvolution) *Handler {
	if h != nil {
		h.evolution = evolution
	}
	return h
}

func (h *Handler) Snapshot(c *gin.Context) {
	ownerID, ok := owner(c)
	if !ok {
		return
	}
	snapshot, err := h.service.Snapshot(c.Request.Context(), ownerID)
	if err != nil {
		writeError(c, err)
		return
	}
	value, err := decodeSnapshot(snapshot)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.evolution != nil {
		value.Evolution = h.evolution.Snapshot()
	}
	c.JSON(http.StatusOK, value)
}

func (h *Handler) Preference(c *gin.Context) {
	ownerID, ok := owner(c)
	if !ok {
		return
	}
	var input struct {
		Enabled          bool  `json:"enabled"`
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if !decode(c, &input) {
		return
	}
	value, err := h.service.SetPreference(c.Request.Context(), ownerID, ownerID, input.Enabled, input.ExpectedRevision)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (h *Handler) Propose(c *gin.Context) {
	ownerID, ok := owner(c)
	if !ok {
		return
	}
	var input struct {
		Kind                 string                         `json:"kind"`
		SourceExperienceRefs []string                       `json:"source_experience_refs"`
		SourceRunRefs        []string                       `json:"source_run_refs"`
		PolicyArtifact       *dso.DelegationPolicyArtifact  `json:"policy_artifact"`
		ProfileArtifact      *dso.SpecialistProfileArtifact `json:"profile_artifact"`
	}
	if !decode(c, &input) {
		return
	}
	value, err := h.service.ProposeCandidate(c.Request.Context(), delegationsvc.LearningCandidateInput{
		OwnerID: ownerID, Kind: input.Kind, SourceExperienceRefs: input.SourceExperienceRefs,
		SourceRunRefs: input.SourceRunRefs, PolicyArtifact: input.PolicyArtifact, ProfileArtifact: input.ProfileArtifact,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

func (h *Handler) EvaluateOffline(c *gin.Context) {
	ownerID, ok := owner(c)
	if !ok {
		return
	}
	var input struct {
		Replays   []dso.ReplayRequest            `json:"replays"`
		Baseline  dso.DelegationBenchmarkMetrics `json:"baseline"`
		Candidate dso.DelegationBenchmarkMetrics `json:"candidate"`
	}
	if !decode(c, &input) {
		return
	}
	value, err := h.service.EvaluateOffline(c.Request.Context(), delegationsvc.OfflineEvaluationRequest{
		OwnerID: ownerID, CandidateID: c.Param("candidate_id"), Replays: input.Replays, Baseline: input.Baseline, Candidate: input.Candidate,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

func (h *Handler) Review(c *gin.Context) {
	ownerID, ok := owner(c)
	if !ok {
		return
	}
	var input struct {
		Decision string   `json:"decision"`
		Reasons  []string `json:"reasons"`
	}
	if !decode(c, &input) {
		return
	}
	value, err := h.service.Review(c.Request.Context(), ownerID, c.Param("candidate_id"), ownerID, input.Decision, input.Reasons)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

func (h *Handler) Shadow(c *gin.Context) {
	ownerID, ok := owner(c)
	if !ok {
		return
	}
	value, err := h.service.EvaluateShadow(c.Request.Context(), ownerID, c.Param("candidate_id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

func (h *Handler) Canary(c *gin.Context) {
	ownerID, ok := owner(c)
	if !ok {
		return
	}
	var input struct {
		AllowedOwnerIDs []string `json:"allowed_owner_ids"`
		Percent         int      `json:"percent"`
	}
	if !decode(c, &input) {
		return
	}
	value, err := h.service.StartCanary(c.Request.Context(), delegationsvc.CanaryRequest{OwnerID: ownerID, CandidateID: c.Param("candidate_id"), AllowedOwnerIDs: input.AllowedOwnerIDs, Percent: input.Percent, ApprovedBy: ownerID})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

func (h *Handler) Benchmark(c *gin.Context) {
	ownerID, ok := owner(c)
	if !ok {
		return
	}
	var report dso.DelegationBenchmarkReport
	if !decode(c, &report) {
		return
	}
	value, err := h.service.RecordBenchmark(c.Request.Context(), ownerID, c.Param("rollout_id"), report)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

func (h *Handler) Promote(c *gin.Context) {
	ownerID, ok := owner(c)
	if !ok {
		return
	}
	value, err := h.service.Promote(c.Request.Context(), ownerID, c.Param("rollout_id"), ownerID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (h *Handler) Disable(c *gin.Context) {
	ownerID, ok := owner(c)
	if !ok {
		return
	}
	value, err := h.service.Disable(c.Request.Context(), ownerID, c.Param("rollout_id"), ownerID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

type snapshotResponse struct {
	Preference  dso.DelegationLearningPreference        `json:"preference"`
	Candidates  []dso.DelegationLearningCandidate       `json:"candidates"`
	Evaluations []dso.DelegationEvaluationResult        `json:"evaluations"`
	Reviews     []dso.DelegationReviewDecision          `json:"reviews"`
	Rollouts    []dso.DelegationLearningRollout         `json:"rollouts"`
	Benchmarks  []dso.DelegationBenchmarkReport         `json:"benchmarks"`
	Evolution   delegationsvc.GovernedEvolutionSnapshot `json:"evolution"`
}

func decodeSnapshot(value delegationentity.LearningSnapshot) (snapshotResponse, error) {
	result := snapshotResponse{
		Preference:  dso.DelegationLearningPreference{Schema: dso.Schema, OwnerID: value.Preference.OwnerID, Enabled: value.Preference.Enabled, Revision: value.Preference.Revision, UpdatedBy: value.Preference.UpdatedBy, UpdatedAt: value.Preference.UpdatedAt},
		Candidates:  make([]dso.DelegationLearningCandidate, 0, len(value.Candidates)),
		Evaluations: make([]dso.DelegationEvaluationResult, 0, len(value.Evaluations)),
		Reviews:     make([]dso.DelegationReviewDecision, 0, len(value.Reviews)),
		Rollouts:    make([]dso.DelegationLearningRollout, 0, len(value.Rollouts)),
		Benchmarks:  make([]dso.DelegationBenchmarkReport, 0, len(value.Benchmarks)),
	}
	for _, record := range value.Candidates {
		var item dso.DelegationLearningCandidate
		if err := json.Unmarshal([]byte(record.Content), &item); err != nil {
			return snapshotResponse{}, err
		}
		result.Candidates = append(result.Candidates, item)
	}
	for _, record := range value.Evaluations {
		var item dso.DelegationEvaluationResult
		if err := json.Unmarshal([]byte(record.Content), &item); err != nil {
			return snapshotResponse{}, err
		}
		result.Evaluations = append(result.Evaluations, item)
	}
	for _, record := range value.Reviews {
		var item dso.DelegationReviewDecision
		if err := json.Unmarshal([]byte(record.Content), &item); err != nil {
			return snapshotResponse{}, err
		}
		result.Reviews = append(result.Reviews, item)
	}
	for _, record := range value.Rollouts {
		var item dso.DelegationLearningRollout
		if err := json.Unmarshal([]byte(record.Content), &item); err != nil {
			return snapshotResponse{}, err
		}
		result.Rollouts = append(result.Rollouts, item)
	}
	for _, record := range value.Benchmarks {
		var item dso.DelegationBenchmarkReport
		if err := json.Unmarshal([]byte(record.Content), &item); err != nil {
			return snapshotResponse{}, err
		}
		result.Benchmarks = append(result.Benchmarks, item)
	}
	return result, nil
}

func owner(c *gin.Context) (string, bool) {
	ownerID := strings.TrimSpace(authctx.UserID(c.Request.Context()))
	if ownerID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated owner is required"})
		return "", false
	}
	return ownerID, true
}

func decode(c *gin.Context, destination any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return false
	}
	return true
}

func writeError(c *gin.Context, err error) {
	status := http.StatusUnprocessableEntity
	if errors.Is(err, delegationrepo.ErrRevisionConflict) || errors.Is(err, delegationrepo.ErrIdempotencyConflict) {
		status = http.StatusConflict
	}
	if strings.Contains(strings.ToLower(err.Error()), "not found") {
		status = http.StatusNotFound
	}
	if errors.Is(err, delegationsvc.ErrDelegationLearningDisabled) {
		status = http.StatusPreconditionFailed
	}
	c.Header("X-Athena-Error-Code", strconv.Itoa(status))
	c.JSON(status, gin.H{"error": err.Error()})
}
