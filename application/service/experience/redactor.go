package experience

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

const (
	redactedCredential = "[REDACTED:CREDENTIAL]"
	redactedPII        = "[REDACTED:PII]"
	redactedPayment    = "[REDACTED:PAYMENT]"
	redactedIdentity   = "[REDACTED:IDENTITY]"
	maximumStringRunes = 4096
)

type redactionPattern struct {
	category    string
	replacement string
	pattern     *regexp.Regexp
}

type Redactor struct {
	patterns       []redactionPattern
	secretKey      *regexp.Regexp
	paymentKey     *regexp.Regexp
	identityKey    *regexp.Regexp
	rawArtifactKey *regexp.Regexp
}

func NewRedactor() *Redactor {
	return &Redactor{
		secretKey:      regexp.MustCompile(`(?i)(?:password|passwd|passphrase|secret|token|api[_-]?key|authorization|cookie|credential|session[_-]?key|private[_-]?key|client[_-]?secret)`),
		paymentKey:     regexp.MustCompile(`(?i)(?:card[_-]?(?:number|no)|cvv|cvc|iban|bank[_-]?account|payment[_-]?(?:token|method))`),
		identityKey:    regexp.MustCompile(`(?i)(?:passport|driver.?s?[_-]?licen[cs]e|identity[_-]?(?:document|number)|national[_-]?id|mynumber)`),
		rawArtifactKey: regexp.MustCompile(`(?i)(?:raw[_-]?)?(?:dom|html|screenshot|image[_-]?data|attachment[_-]?data|page[_-]?source|document[_-]?bytes)`),
		patterns: []redactionPattern{
			{category: "credential", replacement: redactedCredential, pattern: regexp.MustCompile(`(?is)-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----.*?-----END (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`)},
			{category: "credential", replacement: redactedCredential, pattern: regexp.MustCompile(`(?i)\b(?:https?|wss?)://[^/\s:@]+:[^@\s/]+@`)},
			{category: "credential", replacement: redactedCredential, pattern: regexp.MustCompile(`(?i)(?:bearer\s+)[A-Za-z0-9._~+/=-]{8,}`)},
			{category: "credential", replacement: redactedCredential, pattern: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)},
			{category: "credential", replacement: redactedCredential, pattern: regexp.MustCompile(`\b(?:sk|pk|rk|ghp|github_pat|xox[baprs]|hf|npm)[-_][A-Za-z0-9_-]{12,}\b`)},
			{category: "credential", replacement: redactedCredential, pattern: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
			{category: "credential", replacement: redactedCredential, pattern: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}\b`)},
			{category: "credential", replacement: redactedCredential, pattern: regexp.MustCompile(`(?i)(?:password|passwd|passphrase|secret|access[_-]?token|refresh[_-]?token|api[_-]?key|authorization|cookie|credential)\s*[:=]\s*[^\s,;}&]+`)},
			{category: "payment", replacement: redactedPayment, pattern: regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`)},
			{category: "identity", replacement: redactedIdentity, pattern: regexp.MustCompile(`\b(?:[A-Z]{1,2}\d{6,9}|\d{6}(?:19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[0-9Xx])\b`)},
			{category: "pii", replacement: redactedPII, pattern: regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)},
			{category: "pii", replacement: redactedPII, pattern: regexp.MustCompile(`(?:\+?\d[\d ()-]{7,}\d)`)},
		},
	}
}

func (r *Redactor) Sanitize(value any, path string) (any, []entity.Redaction) {
	return r.sanitize(value, normalizePath(path), 0)
}

func (r *Redactor) sanitize(value any, path string, depth int) (any, []entity.Redaction) {
	if value == nil {
		return nil, nil
	}
	if depth > 12 {
		return "[OMITTED:MAX_DEPTH]", nil
	}
	switch typed := value.(type) {
	case string:
		return r.sanitizeString(typed, path)
	case []byte:
		return r.artifactSummary(typed, path)
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		result := make(map[string]any, len(typed))
		redactions := make([]entity.Redaction, 0)
		for _, key := range keys {
			fieldPath := joinPath(path, key)
			if r.rawArtifactKey.MatchString(key) {
				summary, hits := r.artifactSummary([]byte(fmt.Sprint(typed[key])), fieldPath)
				result[key] = summary
				redactions = append(redactions, hits...)
				continue
			}
			if category, replacement := r.sensitiveField(key); category != "" {
				result[key] = replacement
				redactions = append(redactions, newRedaction(category, fieldPath, fmt.Sprint(typed[key])))
				continue
			}
			sanitized, hits := r.sanitize(typed[key], fieldPath, depth+1)
			result[key] = sanitized
			redactions = append(redactions, hits...)
		}
		return result, redactions
	case []any:
		result := make([]any, 0, len(typed))
		redactions := make([]entity.Redaction, 0)
		for index, item := range typed {
			sanitized, hits := r.sanitize(item, fmt.Sprintf("%s[%d]", path, index), depth+1)
			result = append(result, sanitized)
			redactions = append(redactions, hits...)
		}
		return result, redactions
	}

	reflected := reflect.ValueOf(value)
	if reflected.IsValid() && (reflected.Kind() == reflect.Map || reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Struct || reflected.Kind() == reflect.Pointer) {
		return r.sanitize(fmt.Sprint(value), path, depth+1)
	}
	return value, nil
}

func (r *Redactor) sanitizeString(value, path string) (string, []entity.Redaction) {
	result := value
	redactions := make([]entity.Redaction, 0)
	for _, item := range r.patterns {
		result = item.pattern.ReplaceAllStringFunc(result, func(match string) string {
			redactions = append(redactions, newRedaction(item.category, path, match))
			return item.replacement
		})
	}
	runes := []rune(result)
	if len(runes) > maximumStringRunes {
		digest := digest(value)
		result = string(runes[:maximumStringRunes]) + " [TRUNCATED sha256=" + digest + "]"
		redactions = append(redactions, newRedaction("truncation", path, value))
	}
	return result, redactions
}

func (r *Redactor) sensitiveField(key string) (string, string) {
	switch {
	case r.paymentKey.MatchString(key):
		return "payment", redactedPayment
	case r.identityKey.MatchString(key):
		return "identity", redactedIdentity
	case r.secretKey.MatchString(key):
		return "credential", redactedCredential
	default:
		return "", ""
	}
}

func (r *Redactor) artifactSummary(value []byte, path string) (map[string]any, []entity.Redaction) {
	summary := map[string]any{"summary": "raw artifact omitted", "sha256": digestBytes(value), "bytes": len(value)}
	return summary, []entity.Redaction{newRedaction("raw_artifact", path, string(value))}
}

func newRedaction(category, path, original string) entity.Redaction {
	redactionID := ulid.New()
	return entity.Redaction{RedactionID: redactionID, Category: category, FieldPath: normalizePath(path), Digest: digest(redactionID + "\x00" + original), CreatedAt: time.Now().UTC()}
}

func digest(value string) string { return digestBytes([]byte(value)) }

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "$"
	}
	return path
}

func joinPath(parent, key string) string {
	if parent == "" || parent == "$" {
		return "$." + key
	}
	return parent + "." + key
}
