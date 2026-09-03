package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"

	experienceentity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
	learningv2 "github.com/good-fish-man/athena-protocol/protocol/learning/v2"
)

const (
	maximumSynthesisDescriptionLength = 500
	maximumSynthesisSummaryLength     = 500
)

// CandidateSynthesizer proposes a constrained declarative candidate. The
// caller remains responsible for policy validation, replay, and human review.
type CandidateSynthesizer interface {
	Synthesize(context.Context, CandidateSynthesisInput) (CandidateSynthesisResult, error)
	ModelName() string
}

type CandidateSynthesisInput struct {
	CandidateID      string                         `json:"candidate_id"`
	SafetyID         string                         `json:"-"`
	Kind             string                         `json:"kind"`
	Pattern          string                         `json:"pattern"`
	DraftDescription string                         `json:"draft_description"`
	Actions          []CandidateSynthesisAction     `json:"actions"`
	Evidence         []CandidateSynthesisEvidence   `json:"evidence"`
	Capabilities     []CandidateSynthesisCapability `json:"capabilities"`
	PreferredSkill   string                         `json:"preferred_skill"`
	FallbackOrder    []string                       `json:"fallback_order"`
}

type CandidateSynthesisAction struct {
	ID         string   `json:"id"`
	Capability string   `json:"capability"`
	Operation  string   `json:"operation"`
	DependsOn  []string `json:"depends_on"`
}

type CandidateSynthesisEvidence struct {
	Outcome            string `json:"outcome"`
	FailureClass       string `json:"failure_class"`
	Context            string `json:"context"`
	VerificationPassed bool   `json:"verification_passed"`
}

type CandidateSynthesisCapability struct {
	ID         string   `json:"id"`
	Risk       string   `json:"risk"`
	Operations []string `json:"operations"`
}

type CandidateSynthesisResult struct {
	Model       string                     `json:"model"`
	ResponseID  string                     `json:"response_id"`
	InputDigest string                     `json:"input_digest"`
	Proposal    CandidateSynthesisProposal `json:"proposal"`
}

type CandidateSynthesisProposal struct {
	Description       string                        `json:"description"`
	Summary           string                        `json:"summary"`
	Steps             []CandidateSynthesisStep      `json:"steps"`
	RecoveryPaths     []CandidateSynthesisRecovery  `json:"recovery_paths"`
	VerificationRules []CandidateSynthesisRule      `json:"verification_rules"`
	FallbackOrder     []string                      `json:"fallback_order"`
	RetryBudget       CandidateSynthesisRetryBudget `json:"retry_budget"`
}

type CandidateSynthesisStep struct {
	ID         string   `json:"id"`
	Capability string   `json:"capability"`
	Operation  string   `json:"operation"`
	DependsOn  []string `json:"depends_on"`
}

type CandidateSynthesisRecovery struct {
	On          string   `json:"on"`
	StepIDs     []string `json:"step_ids"`
	MaxAttempts int      `json:"max_attempts"`
}

type CandidateSynthesisRule struct {
	Field            string `json:"field"`
	Operator         string `json:"operator"`
	Expected         string `json:"expected"`
	EvidenceRequired bool   `json:"evidence_required"`
}

type CandidateSynthesisRetryBudget struct {
	MaxAttempts   int `json:"max_attempts"`
	MaxDurationMS int `json:"max_duration_ms"`
}

func (s *Service) WithCandidateSynthesizer(value CandidateSynthesizer) *Service {
	if s != nil {
		s.synthesizer = value
	}
	return s
}

func candidateSynthesisInput(ownerID, candidateID, kind, pattern string, selected []experienceentity.Experience, policy entity.ValidationPolicy, skill *entity.SkillDefinition, strategy *entity.StrategyDefinition) CandidateSynthesisInput {
	input := CandidateSynthesisInput{
		CandidateID:   candidateID,
		SafetyID:      synthesisSafetyID(ownerID),
		Kind:          kind,
		Pattern:       pattern,
		Actions:       make([]CandidateSynthesisAction, 0),
		Evidence:      make([]CandidateSynthesisEvidence, 0, len(selected)),
		Capabilities:  make([]CandidateSynthesisCapability, 0),
		FallbackOrder: make([]string, 0),
	}
	if skill != nil {
		input.DraftDescription = "Reusable declarative skill for the supplied semantic action pattern."
		for _, step := range skill.TaskGraphTemplate.Steps {
			input.Actions = append(input.Actions, CandidateSynthesisAction{
				ID: step.ID, Capability: step.Capability, Operation: step.Operation,
				DependsOn: append([]string(nil), step.DependsOn...),
			})
		}
	}
	if strategy != nil {
		input.DraftDescription = "Reusable declarative strategy for the supplied approved skill set."
		input.PreferredSkill = strategy.PreferredSkill
		input.FallbackOrder = append(input.FallbackOrder, strategy.FallbackOrder...)
	}

	contextAliases := make(map[string]string)
	for _, item := range selected {
		contextKey := strings.TrimSpace(item.EnvironmentFingerprint) + "\x00" + evidenceSiteScope(item)
		alias, ok := contextAliases[contextKey]
		if !ok {
			alias = fmt.Sprintf("context-%d", len(contextAliases)+1)
			contextAliases[contextKey] = alias
		}
		failureClass := ""
		if item.Failure != nil {
			failureClass = safeFailureClass(item.Failure.Class)
		}
		input.Evidence = append(input.Evidence, CandidateSynthesisEvidence{
			Outcome: item.Outcome, FailureClass: failureClass, Context: alias,
			VerificationPassed: item.Verification.Passed,
		})
	}

	capabilityIDs := make([]string, 0, len(policy.Capabilities))
	for capabilityID := range policy.Capabilities {
		capabilityIDs = append(capabilityIDs, capabilityID)
	}
	sort.Strings(capabilityIDs)
	for _, capabilityID := range capabilityIDs {
		capabilityPolicy := policy.Capabilities[capabilityID]
		if !capabilityPolicy.Enabled {
			continue
		}
		input.Capabilities = append(input.Capabilities, CandidateSynthesisCapability{
			ID: capabilityID, Risk: capabilityPolicy.Risk,
			Operations: append([]string(nil), capabilityPolicy.Operations...),
		})
	}
	return input
}

func applyCandidateSynthesis(skill *entity.SkillDefinition, strategy *entity.StrategyDefinition, selected []experienceentity.Experience, result CandidateSynthesisResult) error {
	description, err := boundedSynthesisText(result.Proposal.Description, maximumSynthesisDescriptionLength)
	if err != nil {
		return fmt.Errorf("synthesized description: %w", err)
	}
	if _, err := boundedSynthesisText(result.Proposal.Summary, maximumSynthesisSummaryLength); err != nil {
		return fmt.Errorf("synthesized summary: %w", err)
	}

	switch {
	case skill != nil && strategy == nil:
		if err := applySkillSynthesis(skill, selected, result.Proposal); err != nil {
			return err
		}
		skill.Description = description
		if skill.Metadata == nil {
			skill.Metadata = make(map[string]string)
		}
		skill.Metadata["synthesizer"] = "codex"
		skill.Metadata["synthesis_model"] = compactMetadata(result.Model)
		skill.Metadata["synthesis_response_id"] = compactMetadata(result.ResponseID)
		skill.Metadata["synthesis_input_digest"] = compactMetadata(result.InputDigest)
		return nil
	case strategy != nil && skill == nil:
		if err := applyStrategySynthesis(strategy, result.Proposal); err != nil {
			return err
		}
		strategy.Description = description
		return nil
	default:
		return fmt.Errorf("candidate synthesis requires exactly one declarative artifact")
	}
}

func applySkillSynthesis(skill *entity.SkillDefinition, selected []experienceentity.Experience, proposal CandidateSynthesisProposal) error {
	if len(proposal.FallbackOrder) != 0 || proposal.RetryBudget != (CandidateSynthesisRetryBudget{}) {
		return fmt.Errorf("skill synthesis returned strategy-only fields")
	}
	base := make(map[string]entity.TaskStep, len(skill.TaskGraphTemplate.Steps))
	for _, step := range skill.TaskGraphTemplate.Steps {
		base[step.ID] = step
	}
	if len(proposal.Steps) != len(base) {
		return fmt.Errorf("skill synthesis must return every server-provided step exactly once")
	}
	steps := make([]entity.TaskStep, 0, len(proposal.Steps))
	seen := make(map[string]struct{}, len(proposal.Steps))
	for _, proposed := range proposal.Steps {
		original, ok := base[proposed.ID]
		if !ok || original.Capability != proposed.Capability || original.Operation != proposed.Operation {
			return fmt.Errorf("skill synthesis changed the identity of step %q", proposed.ID)
		}
		if _, duplicate := seen[proposed.ID]; duplicate {
			return fmt.Errorf("skill synthesis duplicated step %q", proposed.ID)
		}
		seen[proposed.ID] = struct{}{}
		original.DependsOn = uniqueStrings(proposed.DependsOn)
		steps = append(steps, original)
	}

	allowedFailures := observedFailureClasses(selected)
	recovery := make([]entity.RecoveryPath, 0, len(proposal.RecoveryPaths))
	for _, proposed := range proposal.RecoveryPaths {
		on := safeFailureClass(proposed.On)
		if _, ok := allowedFailures[on]; !ok {
			return fmt.Errorf("skill synthesis introduced unobserved failure class %q", proposed.On)
		}
		if proposed.MaxAttempts < 1 || proposed.MaxAttempts > 3 {
			return fmt.Errorf("skill synthesis recovery attempts must be between one and three")
		}
		stepIDs := uniqueStrings(proposed.StepIDs)
		if len(stepIDs) == 0 {
			return fmt.Errorf("skill synthesis recovery path %q has no steps", on)
		}
		for _, stepID := range stepIDs {
			if _, ok := base[stepID]; !ok {
				return fmt.Errorf("skill synthesis recovery path references unknown step %q", stepID)
			}
		}
		recovery = append(recovery, entity.RecoveryPath{On: on, StepIDs: stepIDs, MaxAttempts: proposed.MaxAttempts})
	}
	if len(recovery) == 0 {
		return fmt.Errorf("skill synthesis must cover observed failure conditions")
	}

	rules, err := safeVerificationRules(proposal.VerificationRules)
	if err != nil {
		return err
	}
	skill.TaskGraphTemplate.Steps = steps
	skill.RecoveryPaths = recovery
	skill.VerificationRules = rules
	return nil
}

func applyStrategySynthesis(strategy *entity.StrategyDefinition, proposal CandidateSynthesisProposal) error {
	if len(proposal.Steps) != 0 || len(proposal.RecoveryPaths) != 0 {
		return fmt.Errorf("strategy synthesis returned skill-only fields")
	}
	allowed := make(map[string]struct{}, len(strategy.FallbackOrder))
	for _, skillID := range strategy.FallbackOrder {
		allowed[skillID] = struct{}{}
	}
	fallbacks := uniqueStrings(proposal.FallbackOrder)
	if len(fallbacks) != len(proposal.FallbackOrder) {
		return fmt.Errorf("strategy synthesis returned duplicate fallback skills")
	}
	for _, skillID := range fallbacks {
		if _, ok := allowed[skillID]; !ok || skillID == strategy.PreferredSkill {
			return fmt.Errorf("strategy synthesis introduced unapproved fallback skill %q", skillID)
		}
	}
	if proposal.RetryBudget.MaxAttempts < 1 || proposal.RetryBudget.MaxAttempts > 3 ||
		proposal.RetryBudget.MaxDurationMS < 1 || proposal.RetryBudget.MaxDurationMS > 120000 {
		return fmt.Errorf("strategy synthesis retry budget exceeds AI evolution limits")
	}
	rules, err := safeVerificationRules(proposal.VerificationRules)
	if err != nil {
		return err
	}
	strategy.FallbackOrder = fallbacks
	strategy.RetryBudget = learningv2.RetryBudget{
		MaxAttempts: proposal.RetryBudget.MaxAttempts, MaxDurationMS: proposal.RetryBudget.MaxDurationMS,
	}
	strategy.VerificationPolicy = rules
	return nil
}

func safeVerificationRules(values []CandidateSynthesisRule) ([]entity.VerificationRule, error) {
	if len(values) == 0 || len(values) > 4 {
		return nil, fmt.Errorf("candidate synthesis requires one to four verification rules")
	}
	result := make([]entity.VerificationRule, 0, len(values))
	requiredCompletion := false
	for _, value := range values {
		field := strings.TrimSpace(value.Field)
		operator := strings.ToLower(strings.TrimSpace(value.Operator))
		expected := strings.TrimSpace(value.Expected)
		if field != "task.status" || operator != "equals" || expected != "COMPLETED" {
			return nil, fmt.Errorf("candidate synthesis may only require task.status equals COMPLETED")
		}
		if !value.EvidenceRequired {
			return nil, fmt.Errorf("candidate synthesis verification must require evidence")
		}
		requiredCompletion = true
		result = append(result, entity.VerificationRule{
			Field: field, Operator: operator, Expected: expected, EvidenceRequired: true,
		})
	}
	if !requiredCompletion {
		return nil, fmt.Errorf("candidate synthesis omitted completion verification")
	}
	return result, nil
}

func observedFailureClasses(selected []experienceentity.Experience) map[string]struct{} {
	result := make(map[string]struct{})
	for _, item := range selected {
		if item.Outcome != experienceentity.OutcomeFailed {
			continue
		}
		value := "OUTCOME_FAILED"
		if item.Failure != nil {
			value = safeFailureClass(item.Failure.Class)
		}
		result[value] = struct{}{}
	}
	return result
}

func safeFailureClass(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "OUTCOME_FAILED"
	}
	for _, char := range value {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return "OUTCOME_FAILED"
		}
	}
	return value
}

func boundedSynthesisText(value string, maximum int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("value is required")
	}
	if len([]rune(value)) > maximum {
		return "", fmt.Errorf("value exceeds %d characters", maximum)
	}
	for _, char := range value {
		if unicode.IsControl(char) && char != '\n' && char != '\t' {
			return "", fmt.Errorf("value contains control characters")
		}
	}
	return value, nil
}

func compactMetadata(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 160 {
		return value[:160]
	}
	return value
}

func synthesisSafetyID(ownerID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(ownerID)))
	return "athena-" + hex.EncodeToString(digest[:16])
}

func skillActionPattern(skill entity.SkillDefinition) string {
	parts := make([]string, 0, len(skill.TaskGraphTemplate.Steps))
	for _, step := range skill.TaskGraphTemplate.Steps {
		parts = append(parts, strings.TrimSpace(step.Capability)+"."+strings.TrimSpace(step.Operation))
	}
	return strings.Join(parts, "|")
}
