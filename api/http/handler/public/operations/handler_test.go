package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	operationssvc "github.com/good-fish-man/agent-runtime-client/application/service/operations"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	ga "github.com/good-fish-man/athena-protocol/protocol/ga/v1"
)

type evidenceStore struct {
	owner string
	items []ga.GoldenJourneyResult
}

func (s *evidenceStore) SaveGoldenJourneyResults(_ context.Context, owner string, items []ga.GoldenJourneyResult) error {
	s.owner = owner
	s.items = append([]ga.GoldenJourneyResult(nil), items...)
	return nil
}

func (s *evidenceStore) LastGoldenJourneyResults(context.Context, string, string) ([]ga.GoldenJourneyResult, error) {
	return nil, nil
}

func TestRecordGoldenJourneyEvidenceRequiresInternalRunnerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(consts.EnvInternalServiceToken, "runner-secret")
	store := &evidenceStore{}
	handler := NewHandler(operationssvc.New("", nil, nil).WithGAEvidenceStore(store))
	payload, err := json.Marshal(map[string]any{"owner_id": "owner-1", "items": completeHandlerE2EResults("run-1")})
	if err != nil {
		t.Fatal(err)
	}

	unauthorized := httptest.NewRecorder()
	unauthorizedRequest := httptest.NewRequest(http.MethodPost, "/internal/operations/golden-journeys/evidence", bytes.NewReader(payload))
	unauthorizedContext, _ := gin.CreateTestContext(unauthorized)
	unauthorizedContext.Request = unauthorizedRequest
	handler.RecordGoldenJourneyEvidenceInternal(unauthorizedContext)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	authorized := httptest.NewRecorder()
	authorizedRequest := httptest.NewRequest(http.MethodPost, "/internal/operations/golden-journeys/evidence", bytes.NewReader(payload))
	authorizedRequest.Header.Set(consts.HeaderAthenaInternalToken, "runner-secret")
	authorizedContext, _ := gin.CreateTestContext(authorized)
	authorizedContext.Request = authorizedRequest
	handler.RecordGoldenJourneyEvidenceInternal(authorizedContext)
	if authorized.Code != http.StatusCreated {
		t.Fatalf("authorized status = %d, want %d: %s", authorized.Code, http.StatusCreated, authorized.Body.String())
	}
	if store.owner != "owner-1" || len(store.items) != len(ga.GoldenJourneys()) {
		t.Fatalf("persisted owner/items = %q/%d", store.owner, len(store.items))
	}
}

func TestListBackupsReportsUnconfiguredCapabilityWithoutFailingOperations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(operationssvc.New("", nil, nil))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/operations/backups", nil)
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request
	ctx.Set(consts.CtxKeyAdminLevel, 1)

	handler.ListBackups(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Items      []json.RawMessage `json:"items"`
		Configured bool              `json:"configured"`
		Reason     string            `json:"reason"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Configured || len(response.Items) != 0 || response.Reason == "" {
		t.Fatalf("unexpected backup capability response: %+v", response)
	}
}

func completeHandlerE2EResults(runID string) []ga.GoldenJourneyResult {
	now := time.Now().UTC()
	results := make([]ga.GoldenJourneyResult, 0, len(ga.GoldenJourneys()))
	for _, journey := range ga.GoldenJourneys() {
		steps := make([]ga.GoldenJourneyStepResult, 0, len(journey.Steps))
		for _, definition := range journey.Steps {
			evidence := make([]ga.EvidenceRef, 0, len(definition.ExpectedEvidence))
			for _, kind := range definition.ExpectedEvidence {
				evidence = append(evidence, ga.EvidenceRef{Kind: kind, Reference: kind + "-value"})
			}
			steps = append(steps, ga.GoldenJourneyStepResult{
				StepID: definition.ID, Status: ga.StatusPass, Message: "verified", Evidence: evidence, DurationMS: 1,
			})
		}
		results = append(results, ga.GoldenJourneyResult{
			RunID: runID, JourneyID: journey.ID, VerificationLevel: ga.VerificationE2E,
			Status: ga.StatusPass, Steps: steps, StartedAt: now, FinishedAt: now.Add(time.Millisecond),
		})
	}
	return results
}
