package experience

import (
	"strings"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
)

type failureRule struct {
	name       string
	class      string
	confidence float64
	keywords   []string
}

var failureRules = []failureRule{
	{name: "user_cancelled", class: entity.FailureUserInterruption, confidence: 1, keywords: []string{"cancelled by user", "user cancelled", "user interrupted"}},
	{name: "device_offline", class: entity.FailureDeviceOffline, confidence: 1, keywords: []string{"device is offline", "device offline", "no connected device"}},
	{name: "policy_denied", class: entity.FailurePolicy, confidence: .98, keywords: []string{"policy denied", "permission denied", "approval rejected", "not allowed by policy"}},
	{name: "model_call", class: entity.FailureModel, confidence: .95, keywords: []string{"model call", "chat/completions", "llm returned", "model stream", "unsupported value: 'temperature'"}},
	{name: "routing", class: entity.FailureRouting, confidence: .93, keywords: []string{"route plan", "resolve route", "no route", "primary route"}},
	{name: "planning", class: entity.FailurePlanning, confidence: .92, keywords: []string{"planner", "task graph", "plan generation"}},
	{name: "capability_selection", class: entity.FailureCapabilitySelection, confidence: .94, keywords: []string{"capability is unsupported", "capability unsupported", "no capability instance"}},
	{name: "argument_validation", class: entity.FailureArgument, confidence: .96, keywords: []string{"invalid argument", "arguments are required", "validation failed", "missing required"}},
	{name: "perception", class: entity.FailurePerception, confidence: .94, keywords: []string{"element not found", "page content", "observation budget", "screenshot failed", "dom snapshot"}},
	{name: "verification", class: entity.FailureVerification, confidence: .96, keywords: []string{"verification failed", "playback not verified", "expected observation"}},
	{name: "environment_drift", class: entity.FailureEnvironmentDrift, confidence: .9, keywords: []string{"stale target", "target closed", "environment changed", "revision conflict"}},
	{name: "intent", class: entity.FailureIntent, confidence: .85, keywords: []string{"intent parse", "ambiguous intent", "intent missing"}},
}

func classifyFailure(task *controlentity.TaskSession) *entity.FailureClassification {
	if task == nil || task.Status == controlentity.StatusCompleted {
		return nil
	}
	if task.Status == controlentity.StatusCancelled {
		return &entity.FailureClassification{Class: entity.FailureUserInterruption, Rule: "terminal_status_cancelled", Summary: "Task was cancelled before completion.", Confidence: 1}
	}
	text, evidenceIDs := failureEvidence(task)
	lower := strings.ToLower(text)
	for _, rule := range failureRules {
		for _, keyword := range rule.keywords {
			if strings.Contains(lower, keyword) {
				return &entity.FailureClassification{Class: rule.class, Rule: rule.name, Summary: compactFailureSummary(text), EvidenceIDs: evidenceIDs, Confidence: rule.confidence}
			}
		}
	}
	return &entity.FailureClassification{Class: entity.FailureRuntime, Rule: "fallback_runtime_failure", Summary: compactFailureSummary(text), EvidenceIDs: evidenceIDs, Confidence: .6}
}

func failureEvidence(task *controlentity.TaskSession) (string, []string) {
	parts := make([]string, 0)
	evidence := make([]string, 0)
	for detail := task.ErrorDetail; detail != nil; detail = detail.Cause {
		parts = append(parts, detail.Code, detail.Operation, detail.Message)
	}
	for _, observation := range task.Observations {
		if observation.Status != controlentity.ObservationFailed && observation.Status != controlentity.ObservationBlocked && observation.Status != controlentity.ObservationExpired {
			continue
		}
		parts = append(parts, observation.Error, observation.Summary)
		for detail := observation.ErrorDetail; detail != nil; detail = detail.Cause {
			parts = append(parts, detail.Code, detail.Operation, detail.Message)
		}
		for _, item := range observation.Evidence {
			if item.EvidenceID != "" {
				evidence = append(evidence, item.EvidenceID)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, " ")), evidence
}

func compactFailureSummary(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "Task failed without a structured error detail."
	}
	runes := []rune(value)
	if len(runes) > 512 {
		return string(runes[:512]) + "..."
	}
	return value
}
