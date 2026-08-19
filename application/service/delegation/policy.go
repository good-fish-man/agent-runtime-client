package delegation

import (
	"context"
	"regexp"
	"strings"
)

type DelegationRoute string

const (
	RouteFastPath   DelegationRoute = "fast_path"
	RouteSpecialist DelegationRoute = "single_specialist"
)

type JudgmentRequest struct {
	Prompt                string
	RuleScore             int
	RequestedCapabilities []string
}

type Judgment struct {
	Delegate bool
	Reasons  []string
}

type DelegationJudge interface {
	Judge(context.Context, JudgmentRequest) (Judgment, error)
}

type RouteDecision struct {
	Route                 DelegationRoute
	Reasons               []string
	RequestedCapabilities []string
}

type RoutePolicy struct{ judge DelegationJudge }

func NewRoutePolicy(judge DelegationJudge) *RoutePolicy { return &RoutePolicy{judge: judge} }

var (
	researchSignals = regexp.MustCompile(`(?i)\b(compare|comparison|research|investigate|evaluate|analy[sz]e|trade[- ]?offs?|pros and cons|latest|current|evidence|sources?)\b|比较|对比|调研|研究|分析|评估|最新|现状|证据|来源|比較|調査|分析|評価|最新`)
	multiSubject    = regexp.MustCompile(`(?i)\b(vs\.?|versus|and|or)\b|、|以及|与|和|及|と|および`)
	directAction    = regexp.MustCompile(`(?i)^\s*(open|click|press|play|pause|close|type|scroll|navigate|download|upload)\b|^\s*(打开|点击|播放|暂停|关闭|输入|滚动|下载|上传|開く|クリック|再生|停止)`)
)

// Decide keeps ordinary conversation on the zero-allocation fast path. Rules
// settle clear cases locally; an optional bounded judge is consulted only for
// ambiguous research requests, never for direct device commands.
func (p *RoutePolicy) Decide(ctx context.Context, prompt string) RouteDecision {
	value := strings.TrimSpace(prompt)
	if value == "" || directAction.MatchString(value) {
		return RouteDecision{Route: RouteFastPath, Reasons: []string{"direct_or_empty_request"}}
	}
	score := 0
	reasons := make([]string, 0, 3)
	if researchSignals.MatchString(value) {
		score += 2
		reasons = append(reasons, "research_intent")
	}
	if multiSubject.MatchString(value) {
		score++
		reasons = append(reasons, "multiple_subjects")
	}
	if len([]rune(value)) >= 120 {
		score++
		reasons = append(reasons, "compound_request")
	}
	requested := []string{"internet.search", "internet.fetch"}
	if score >= 3 {
		return RouteDecision{Route: RouteSpecialist, Reasons: reasons, RequestedCapabilities: requested}
	}
	if score == 2 && p != nil && p.judge != nil {
		judgment, err := p.judge.Judge(ctx, JudgmentRequest{Prompt: value, RuleScore: score, RequestedCapabilities: requested})
		if err == nil && judgment.Delegate {
			return RouteDecision{Route: RouteSpecialist, Reasons: append(reasons, judgment.Reasons...), RequestedCapabilities: requested}
		}
	}
	return RouteDecision{Route: RouteFastPath, Reasons: append(reasons, "coordination_cost_exceeds_expected_gain")}
}

func admitCapabilities(parent, requested []string) []string {
	allowed := make(map[string]struct{}, len(parent))
	for _, capability := range parent {
		allowed[strings.TrimSpace(capability)] = struct{}{}
	}
	result := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, capability := range requested {
		capability = strings.TrimSpace(capability)
		if !isReadOnlyResearchCapability(capability) {
			continue
		}
		if _, ok := allowed[capability]; !ok {
			continue
		}
		if _, duplicate := seen[capability]; duplicate {
			continue
		}
		seen[capability] = struct{}{}
		result = append(result, capability)
	}
	return result
}

func isReadOnlyResearchCapability(value string) bool {
	switch value {
	case "internet.search", "internet.fetch", "github.search", "browser.search", "browser.read", "browser.observe", "filesystem.read", "filesystem.search", "filesystem.list":
		return true
	default:
		return false
	}
}
