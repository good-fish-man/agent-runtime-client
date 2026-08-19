package delegation

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

const defaultContextBytes int64 = 32 * 1024

type ContextSource struct {
	Ref            string
	OwnerID        string
	SourceType     string
	TrustClass     string
	Classification string
	Content        string
}

type ContextBundle struct {
	Slice   dso.RedactedContextSlice `json:"slice"`
	Payload map[string]string        `json:"payload"`
}

type ContextBuilder struct{ now func() time.Time }

func NewContextBuilder() *ContextBuilder {
	return &ContextBuilder{now: func() time.Time { return time.Now().UTC() }}
}

var (
	secretKeyPattern   = regexp.MustCompile(`(?i)(api[_-]?key|password|passwd|secret|access[_-]?token|refresh[_-]?token|authorization|cookie|private[_-]?key|client[_-]?secret)`)
	secretTextPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/-]{8,}={0,2}`),
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`),
		regexp.MustCompile(`-----BEGIN(?: [A-Z]+)? PRIVATE KEY-----`),
	}
	injectionPattern = regexp.MustCompile(`(?i)(ignore|disregard|override).{0,40}(previous|system|developer|instruction)|忽略.{0,20}(之前|系统|指令)|无视.{0,20}(以前|システム|指示)`)
)

var runtimeContextAllowlist = map[string]struct{}{
	"locale": {}, "timezone": {}, "location": {}, "country": {},
	"knowledge_context": {}, "knowledge_evidence": {}, "research_context": {},
	"research_evidence": {}, "world_slice": {}, "task_constraints": {},
}

func (b *ContextBuilder) Build(ownerID, runID string, scope dso.ContextScope, values map[string]any, sources ...ContextSource) (ContextBundle, error) {
	if strings.TrimSpace(ownerID) == "" || strings.TrimSpace(runID) == "" {
		return ContextBundle{}, fmt.Errorf("context owner and run are required")
	}
	for key, value := range values {
		if _, allowed := runtimeContextAllowlist[key]; !allowed {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return ContextBundle{}, fmt.Errorf("encode context %s: %w", key, err)
		}
		sources = append(sources, ContextSource{
			Ref: "context://" + key, OwnerID: ownerID, SourceType: "runtime_context",
			TrustClass: dso.TrustInternal, Classification: dso.ClassInternal, Content: string(encoded),
		})
	}
	maxBytes := scope.MaxBytes
	if maxBytes <= 0 || maxBytes > defaultContextBytes {
		maxBytes = defaultContextBytes
	}
	allowedRefs := stringSet(scope.ContentRefs)
	allowedClasses := stringSet(scope.AllowedClasses)
	bundle := ContextBundle{Payload: make(map[string]string)}
	for _, source := range sources {
		if source.OwnerID != ownerID {
			return ContextBundle{}, fmt.Errorf("context source %q crosses owner boundary", source.Ref)
		}
		if source.Ref == "" || source.SourceType == "" {
			return ContextBundle{}, fmt.Errorf("context source ref and type are required")
		}
		if len(allowedRefs) > 0 {
			if _, ok := allowedRefs[source.Ref]; !ok {
				continue
			}
		}
		if source.Classification == "" {
			source.Classification = dso.ClassInternal
		}
		if len(allowedClasses) > 0 {
			if _, ok := allowedClasses[source.Classification]; !ok {
				continue
			}
		}
		if source.Classification == dso.ClassRestricted {
			continue
		}
		redacted := redactContext(source.Content)
		if strings.TrimSpace(redacted) == "" {
			continue
		}
		remaining := maxBytes - bundle.Slice.TotalBytes
		if remaining <= 0 {
			break
		}
		if int64(len(redacted)) > remaining {
			redacted = truncateUTF8(redacted, remaining)
		}
		trust := source.TrustClass
		if trust == "" {
			trust = dso.TrustInternal
		}
		taints := make([]string, 0, 1)
		if trust == dso.TrustExternal && injectionPattern.MatchString(redacted) {
			taints = append(taints, "prompt_injection_possible")
		}
		digest, err := dso.Hash(redacted)
		if err != nil {
			return ContextBundle{}, err
		}
		item := dso.ContextItem{
			ContentRef: source.Ref, SourceType: source.SourceType, TrustClass: trust,
			TaintFlags: taints, Classification: source.Classification, OwnerRef: ownerID, ContentHash: digest,
		}
		if err := item.Validate(ownerID); err != nil {
			return ContextBundle{}, err
		}
		bundle.Slice.Items = append(bundle.Slice.Items, item)
		bundle.Slice.TotalBytes += int64(len(redacted))
		bundle.Payload[source.Ref] = redacted
	}
	sort.Slice(bundle.Slice.Items, func(i, j int) bool { return bundle.Slice.Items[i].ContentRef < bundle.Slice.Items[j].ContentRef })
	bundle.Slice.ContextSliceID = "context-" + runID
	bundle.Slice.OwnerID = ownerID
	bundle.Slice.CreatedAt = b.now().UTC()
	copy := bundle.Slice
	copy.ContentHash = ""
	digest, err := dso.Hash(copy)
	if err != nil {
		return ContextBundle{}, err
	}
	bundle.Slice.ContentHash = digest
	if err := bundle.Slice.Validate(); err != nil {
		return ContextBundle{}, err
	}
	return bundle, nil
}

func truncateUTF8(value string, maxBytes int64) string {
	if int64(len(value)) <= maxBytes {
		return value
	}
	for int64(len(value)) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(value)
		if size <= 0 {
			return ""
		}
		value = value[:len(value)-size]
	}
	return value
}

func redactContext(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) == nil {
		decoded = redactJSON(decoded)
		if encoded, err := json.Marshal(decoded); err == nil {
			value = string(encoded)
		}
	}
	for _, pattern := range secretTextPatterns {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}

func redactJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			if secretKeyPattern.MatchString(key) {
				result[key] = "[REDACTED]"
				continue
			}
			result[key] = redactJSON(child)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = redactJSON(child)
		}
		return result
	case string:
		for _, pattern := range secretTextPatterns {
			typed = pattern.ReplaceAllString(typed, "[REDACTED]")
		}
		return typed
	default:
		return value
	}
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}
