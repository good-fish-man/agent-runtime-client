package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
)

const defaultShadowEvaluatorVersion = "declarative-shadow-v2"

type ShadowEvaluationInput struct {
	TaskID          string
	InputDigest     string
	Input           map[string]any
	Build           entity.AgentBuild
	Budget          entity.RunBudget
	CapabilityHints []string
}

type ShadowEffectCounters struct {
	WorldWrites     int
	NetworkRequests int
	DeviceActions   int
	CredentialReads int
}

type ShadowPlan struct {
	Route               []string
	Graph               []string
	PlannedActions      []string
	EstimatedCostMicros int64
	RiskLevel           string
	Effects             ShadowEffectCounters
}

type ShadowEvaluator interface {
	Version() string
	Evaluate(context.Context, ShadowEvaluationInput) (ShadowPlan, error)
}

type declarativeShadowEvaluator struct{}

func NewDeclarativeShadowEvaluator() ShadowEvaluator { return declarativeShadowEvaluator{} }

func (declarativeShadowEvaluator) Version() string { return defaultShadowEvaluatorVersion }

func (declarativeShadowEvaluator) Evaluate(_ context.Context, input ShadowEvaluationInput) (ShadowPlan, error) {
	if strings.TrimSpace(input.TaskID) == "" || strings.TrimSpace(input.InputDigest) == "" {
		return ShadowPlan{}, fmt.Errorf("shadow task and input digest are required")
	}
	if err := input.Build.Validate(); err != nil {
		return ShadowPlan{}, fmt.Errorf("immutable build is invalid: %w", err)
	}
	route := []string{"planner:" + input.Build.PlannerVersion, "policy:" + input.Build.PolicyVersion}
	graph := []string{"kernel:" + input.Build.KernelVersion}
	actions := make([]string, 0, len(input.Build.SkillVersions)+len(input.Build.StrategyVersions)+1)
	for _, value := range sortedVersions("skill", input.Build.SkillVersions) {
		route = append(route, value)
		actions = append(actions, "plan:"+value)
	}
	for _, value := range sortedVersions("strategy", input.Build.StrategyVersions) {
		route = append(route, value)
		actions = append(actions, "plan:"+value)
	}
	graph = append(graph, sortedVersions("prompt", input.Build.PromptTemplateVersions)...)
	graph = append(graph, "ontology:"+input.Build.OntologyVersion, "evaluation:"+input.Build.EvaluationSuiteVersion)
	if len(input.CapabilityHints) > 0 {
		hints := append([]string(nil), input.CapabilityHints...)
		sort.Strings(hints)
		for _, hint := range hints {
			if hint = strings.TrimSpace(hint); hint != "" {
				graph = append(graph, "capability:"+hint)
			}
		}
	}
	actions = append(actions, "plan:verified-response")
	return ShadowPlan{
		Route: route, Graph: graph, PlannedActions: actions,
		EstimatedCostMicros: int64(500 + len(route)*100 + len(graph)*50 + len(actions)*25),
		RiskLevel:           input.Build.RiskLevel,
	}, nil
}

func sortedVersions(prefix string, values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, prefix+":"+key+"@"+values[key])
	}
	return result
}

func digest(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func proofFor(build entity.AgentBuild, inputDigest string, effects ShadowEffectCounters) (entity.ShadowSideEffectProof, error) {
	proofDigest, err := digest(struct {
		Mode        string
		BuildID     string
		BuildHash   string
		InputDigest string
		Effects     ShadowEffectCounters
	}{Mode: "PLAN_ONLY", BuildID: build.BuildID, BuildHash: build.Checksum, InputDigest: inputDigest, Effects: effects})
	if err != nil {
		return entity.ShadowSideEffectProof{}, err
	}
	return entity.ShadowSideEffectProof{
		Mode: "PLAN_ONLY", WorldWrites: effects.WorldWrites, NetworkRequests: effects.NetworkRequests,
		DeviceActions: effects.DeviceActions, CredentialReads: effects.CredentialReads, ProofDigest: proofDigest,
	}, nil
}

func noEffects(value ShadowEffectCounters) bool {
	return value.WorldWrites == 0 && value.NetworkRequests == 0 && value.DeviceActions == 0 && value.CredentialReads == 0
}

func riskRank(value string) int {
	switch value {
	case entity.RiskR0:
		return 0
	case entity.RiskR1:
		return 1
	case entity.RiskR2:
		return 2
	case entity.RiskR3:
		return 3
	default:
		return 99
	}
}
