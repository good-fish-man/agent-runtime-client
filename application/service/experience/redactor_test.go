package experience

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
)

func TestRedactorSecretCorpusHasZeroLeakage(t *testing.T) {
	redactor := NewRedactor()
	corpus := []string{
		"password=hunter2-secret",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz.1234567890",
		"api_key=sk-test_1234567890abcdefghijklmnop",
		"cookie=sessionid-super-secret-value",
		"contact me at user@example.com",
		"phone +81 90-1234-5678",
		"card 4111 1111 1111 1111",
		"passport AB1234567",
	}
	for _, secret := range corpus {
		sanitized, hits := redactor.Sanitize(secret, "$.value")
		text := sanitized.(string)
		if text == secret || strings.Contains(text, strings.TrimPrefix(secret, "password=")) {
			t.Fatalf("secret leaked: input=%q output=%q", secret, text)
		}
		if len(hits) == 0 {
			t.Fatalf("secret was not recorded as a redaction: %q", secret)
		}
		for _, hit := range hits {
			if hit.Digest == "" || hit.FieldPath != "$.value" {
				t.Fatalf("invalid redaction metadata: %#v", hit)
			}
		}
	}
}

func TestRedactorRemovesStructuredSecretsAndRawArtifacts(t *testing.T) {
	redactor := NewRedactor()
	value, hits := redactor.Sanitize(map[string]any{
		"username": "athena",
		"password": "never-persist-this",
		"nested":   map[string]any{"access_token": "token-value"},
		"raw_dom":  "<html><input value=secret></html>",
	}, "$")
	data, _ := json.Marshal(value)
	text := string(data)
	for _, forbidden := range []string{"never-persist-this", "token-value", "<html>"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("structured secret leaked: %s", text)
		}
	}
	if len(hits) != 3 || !strings.Contains(text, "raw artifact omitted") {
		t.Fatalf("unexpected sanitization result: %s hits=%d", text, len(hits))
	}
}

func TestBuildExperienceSanitizesBeforePayload(t *testing.T) {
	now := time.Now().UTC()
	task := &controlentity.TaskSession{
		TaskID: "task-1", UserID: "user-1", DeviceID: "device-1", Goal: "open account api_key=sk-test_1234567890abcdefghijklmnop",
		Status: controlentity.StatusCompleted, Revision: 2, CreatedAt: now.Add(-time.Second), UpdatedAt: now,
		Metadata:     map[string]interface{}{"intent": map[string]any{"password": "secret-value", "kind": "browser"}},
		Steps:        []controlentity.TaskStep{{StepID: "step-1", TaskID: "task-1", Ordinal: 1, Status: controlentity.StepStatusCompleted, Title: "Open page"}},
		Actions:      []controlentity.Action{{ActionID: "action-1", StepID: "step-1", Capability: "browser.open", Operation: "open", Protocol: "athena.agent.v4", Arguments: map[string]any{"password": "not-copied"}}},
		Observations: []controlentity.Observation{{ObservationID: "observation-1", ActionID: "action-1", Status: controlentity.ObservationSucceeded, Summary: "opened for user@example.com"}},
	}
	service := &Service{redactor: NewRedactor()}
	stored, err := service.build(task, nil, entity.Preference{LearningEnabled: true, RetentionDays: 30, MaxSensitivity: entity.SensitivityRestricted}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Experience.Status != entity.StatusReady || stored.Experience.Sensitivity != entity.SensitivityRestricted {
		t.Fatalf("unexpected experience status: %#v", stored.Experience)
	}
	for _, forbidden := range []string{"sk-test_1234567890", "secret-value", "not-copied", "user@example.com"} {
		if strings.Contains(stored.Payload, forbidden) {
			t.Fatalf("experience payload leaked %q: %s", forbidden, stored.Payload)
		}
	}
	if len(stored.Redactions) < 3 {
		t.Fatalf("expected redaction audit entries, got %d", len(stored.Redactions))
	}
}

func TestBuildExperienceSkipsSensitivityAbovePreference(t *testing.T) {
	now := time.Now().UTC()
	task := &controlentity.TaskSession{TaskID: "task-2", UserID: "user-1", Goal: "password=secret", Status: controlentity.StatusFailed, CreatedAt: now, UpdatedAt: now}
	service := &Service{redactor: NewRedactor()}
	stored, err := service.build(task, nil, entity.Preference{LearningEnabled: true, RetentionDays: 7, MaxSensitivity: entity.SensitivitySensitive}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Experience.Status != entity.StatusSkipped || stored.Experience.SkipReason != "sensitivity_exceeds_user_preference" || stored.Experience.Retention.PayloadMode != entity.PayloadNone || stored.Payload != "" {
		t.Fatalf("restricted experience was not safely skipped: %#v", stored)
	}
}

func TestBuildExperienceHonorsDisabledLearningWithoutReadingContent(t *testing.T) {
	now := time.Now().UTC()
	task := &controlentity.TaskSession{TaskID: "task-disabled", UserID: "user-1", Goal: "api_key=secret-value", Status: controlentity.StatusCompleted, CreatedAt: now, UpdatedAt: now}
	service := &Service{redactor: NewRedactor()}
	stored, err := service.build(task, nil, entity.Preference{LearningEnabled: false, RetentionDays: 7, MaxSensitivity: entity.SensitivityRestricted}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Experience.Status != entity.StatusSkipped || stored.Experience.GoalSummary != "" || stored.Experience.Retention.PayloadMode != entity.PayloadNone || stored.Payload != "" {
		t.Fatalf("disabled learning retained task content: %#v", stored)
	}
}
