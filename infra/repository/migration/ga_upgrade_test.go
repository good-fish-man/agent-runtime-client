package migration

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	chatpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/chat"
	deploymentpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/deployment"
	experiencepo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/experience"
	knowledgepo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/knowledge"
	learningpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/learning"
	memorypo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/memory"
	operationspo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/operations"
	orchestrationpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/orchestration"
	userpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/user"
)

func TestUpgradeFromV09PreservesUserConversationAndMemory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&userpo.SysUser{}, &chatpo.ChatSession{}, &memorypo.AgentMemory{}); err != nil {
		t.Fatal(err)
	}
	user := userpo.SysUser{Ulid: "user-v09", MemberCode: "existing-user", NickName: "Existing"}
	session := chatpo.ChatSession{Ulid: "session-v09", UserId: user.Ulid, AgentId: "agent-v09", Title: "Keep me", Status: "active"}
	memory := memorypo.AgentMemory{Ulid: "memory-v09", UserID: user.Ulid, AgentID: session.AgentId, SessionID: session.Ulid, Name: "Preference", Content: "keep this value", Enabled: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&memory).Error; err != nil {
		t.Fatal(err)
	}

	store := data.New(db)
	if err := InitTables(context.Background(), store, BootstrapAdmin{}); err != nil {
		t.Fatal(err)
	}
	if err := InitTables(context.Background(), store, BootstrapAdmin{}); err != nil {
		t.Fatalf("GA migration is not idempotent: %v", err)
	}

	var userCount, sessionCount, memoryCount int64
	if err := db.Model(&userpo.SysUser{}).Where("ulid = ? AND member_code = ?", user.Ulid, user.MemberCode).Count(&userCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&chatpo.ChatSession{}).Where("ulid = ? AND title = ?", session.Ulid, session.Title).Count(&sessionCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&memorypo.AgentMemory{}).Where("ulid = ? AND content = ?", memory.Ulid, memory.Content).Count(&memoryCount).Error; err != nil {
		t.Fatal(err)
	}
	if userCount != 1 || sessionCount != 1 || memoryCount != 1 {
		t.Fatalf("v0.9 data was not preserved: user=%d session=%d memory=%d", userCount, sessionCount, memoryCount)
	}
	if !db.Migrator().HasTable(&operationspo.GoldenJourneyResult{}) {
		t.Fatal("GA evidence table was not created during upgrade")
	}
	for name, table := range map[string]any{
		"candidate":            &learningpo.Candidate{},
		"candidate_evidence":   &learningpo.CandidateEvidence{},
		"candidate_evaluation": &learningpo.CandidateEvaluation{},
		"skill":                &learningpo.Skill{},
		"skill_version":        &learningpo.SkillVersion{},
		"strategy":             &learningpo.Strategy{},
		"strategy_version":     &learningpo.StrategyVersion{},
		"demonstration":        &learningpo.Demonstration{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("v0.4 learning table %s was not created during upgrade", name)
		}
	}
	for name, table := range map[string]any{
		"goal":             &orchestrationpo.Goal{},
		"goal_task":        &orchestrationpo.GoalTask{},
		"specialist_run":   &orchestrationpo.SpecialistRun{},
		"goal_checkpoint":  &orchestrationpo.GoalCheckpoint{},
		"schedule_trigger": &orchestrationpo.ScheduleTrigger{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("v0.7 orchestration table %s was not created during upgrade", name)
		}
	}
	for name, table := range map[string]any{
		"claim":              &knowledgepo.Claim{},
		"evidence":           &knowledgepo.Evidence{},
		"contradiction":      &knowledgepo.Contradiction{},
		"knowledge_snapshot": &knowledgepo.Snapshot{},
		"ontology_pack":      &knowledgepo.OntologyPack{},
		"ontology_version":   &knowledgepo.OntologyVersion{},
		"ontology_candidate": &knowledgepo.OntologyCandidate{},
		"ontology_migration": &knowledgepo.OntologyMigration{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("v0.6 knowledge table %s was not created during upgrade", name)
		}
	}
	for name, table := range map[string]any{
		"agent_build":   &deploymentpo.AgentBuild{},
		"run_manifest":  &deploymentpo.RunManifest{},
		"promotion":     &deploymentpo.Promotion{},
		"exposure":      &deploymentpo.Exposure{},
		"shadow_result": &deploymentpo.ShadowResult{},
		"canary_metric": &deploymentpo.CanaryMetric{},
		"canary_sample": &deploymentpo.CanarySample{},
		"rollback":      &deploymentpo.Rollback{},
		"compensation":  &deploymentpo.Compensation{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("v0.5 deployment table %s was not created during upgrade", name)
		}
	}
	for name, assertion := range map[string]struct {
		table  any
		column string
	}{
		"skill organization scope":    {table: &learningpo.Skill{}, column: "organization_id"},
		"strategy organization scope": {table: &learningpo.Strategy{}, column: "organization_id"},
	} {
		if !db.Migrator().HasColumn(assertion.table, assertion.column) {
			t.Fatalf("v0.4 %s column was not created during upgrade", name)
		}
	}
}

func TestUpgradeExistingEvaluationRowsAddsComparableBaselineFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyEvaluationRun{}, &legacyEvaluationResult{}); err != nil {
		t.Fatal(err)
	}
	run := legacyEvaluationRun{
		RunID: "legacy-run", OwnerID: "user-1", SuiteID: "suite-1", Status: "COMPLETED", Seed: 42,
		Metrics: `{}`, StartedAt: 1, CreatedAt: 1, UpdatedAt: 1,
	}
	result := legacyEvaluationResult{
		ResultID: "legacy-result", OwnerID: "user-1", RunID: run.RunID, FixtureID: "fixture-1",
		Passed: true, Metrics: `{}`, Summary: "legacy", EvidenceIDs: `[]`, CreatedAt: 1,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&result).Error; err != nil {
		t.Fatal(err)
	}
	if err := InitTables(context.Background(), data.New(db), BootstrapAdmin{}); err != nil {
		t.Fatalf("upgrade existing evaluation rows: %v", err)
	}
	for _, column := range []string{"baseline_metrics", "metric_delta", "regression", "regression_count"} {
		if !db.Migrator().HasColumn(&experiencepo.EvaluationRun{}, column) {
			t.Fatalf("evaluation run column %s was not added", column)
		}
	}
	var upgradedRun experiencepo.EvaluationRun
	if err := db.Where("run_id = ?", run.RunID).First(&upgradedRun).Error; err != nil {
		t.Fatal(err)
	}
	if upgradedRun.BaselineMetrics != `{}` || upgradedRun.MetricDelta != `{}` || upgradedRun.Regression || upgradedRun.RegressionCount != 0 {
		t.Fatalf("legacy evaluation run has unsafe comparison defaults: %#v", upgradedRun)
	}
	var upgradedResult experiencepo.EvaluationResult
	if err := db.Where("result_id = ?", result.ResultID).First(&upgradedResult).Error; err != nil {
		t.Fatal(err)
	}
	if upgradedResult.BaselineMetrics != `{}` || upgradedResult.MetricDelta != `{}` || upgradedResult.Regression {
		t.Fatalf("legacy evaluation result has unsafe comparison defaults: %#v", upgradedResult)
	}
}

func TestUpgradeExistingDemonstrationsAddsConfirmationTimeWithoutForgingConfirmation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyDemonstration{}); err != nil {
		t.Fatal(err)
	}
	demo := legacyDemonstration{
		DemonstrationID: "legacy-demo", OwnerID: "user-1", TaskID: "task-1",
		Status: "CONFIRMED", Title: "Legacy demonstration", Content: `{}`,
		ConfirmedBy: "user-1", Revision: 1, CreatedAt: 1, UpdatedAt: 1,
	}
	if err := db.Create(&demo).Error; err != nil {
		t.Fatal(err)
	}
	store := data.New(db)
	if err := InitTables(context.Background(), store, BootstrapAdmin{}); err != nil {
		t.Fatalf("upgrade existing demonstration: %v", err)
	}
	if err := InitTables(context.Background(), store, BootstrapAdmin{}); err != nil {
		t.Fatalf("demonstration upgrade is not idempotent: %v", err)
	}
	if !db.Migrator().HasColumn(&learningpo.Demonstration{}, "confirmed_at") {
		t.Fatal("demonstration confirmed_at column was not added")
	}
	var upgraded learningpo.Demonstration
	if err := db.Where("demonstration_id = ?", demo.DemonstrationID).First(&upgraded).Error; err != nil {
		t.Fatal(err)
	}
	if upgraded.ConfirmedAt != 0 {
		t.Fatalf("legacy demonstration was given forged confirmation time %d", upgraded.ConfirmedAt)
	}
	if upgraded.ConfirmedBy != demo.ConfirmedBy || upgraded.Title != demo.Title {
		t.Fatalf("legacy demonstration data was not preserved: %#v", upgraded)
	}
}

type legacyEvaluationRun struct {
	RunID       string `gorm:"column:run_id;primaryKey"`
	OwnerID     string `gorm:"column:owner_id"`
	SuiteID     string `gorm:"column:suite_id"`
	Status      string `gorm:"column:status"`
	Seed        int64  `gorm:"column:seed"`
	CandidateID string `gorm:"column:candidate_id"`
	BaselineID  string `gorm:"column:baseline_id"`
	Metrics     string `gorm:"column:metrics"`
	StartedAt   int64  `gorm:"column:started_at"`
	FinishedAt  int64  `gorm:"column:finished_at"`
	Error       string `gorm:"column:error"`
	CreatedAt   int64  `gorm:"column:created_at"`
	UpdatedAt   int64  `gorm:"column:updated_at"`
}

func (*legacyEvaluationRun) TableName() string { return "os_evaluation_run" }

type legacyEvaluationResult struct {
	ResultID    string `gorm:"column:result_id;primaryKey"`
	OwnerID     string `gorm:"column:owner_id"`
	RunID       string `gorm:"column:run_id"`
	FixtureID   string `gorm:"column:fixture_id"`
	Passed      bool   `gorm:"column:passed"`
	Metrics     string `gorm:"column:metrics"`
	Summary     string `gorm:"column:summary"`
	EvidenceIDs string `gorm:"column:evidence_ids"`
	CreatedAt   int64  `gorm:"column:created_at"`
}

func (*legacyEvaluationResult) TableName() string { return "os_evaluation_result" }

type legacyDemonstration struct {
	DemonstrationID string `gorm:"column:demonstration_id;primaryKey"`
	OwnerID         string `gorm:"column:owner_id"`
	TaskID          string `gorm:"column:task_id"`
	Status          string `gorm:"column:status"`
	Title           string `gorm:"column:title"`
	Content         string `gorm:"column:content"`
	PauseCount      int    `gorm:"column:pause_count"`
	ConfirmedBy     string `gorm:"column:confirmed_by"`
	Revision        int64  `gorm:"column:revision"`
	TraceID         string `gorm:"column:trace_id"`
	CreatedAt       int64  `gorm:"column:created_at"`
	UpdatedAt       int64  `gorm:"column:updated_at"`
	DeletedAt       int64  `gorm:"column:deleted_at"`
}

func (*legacyDemonstration) TableName() string { return "os_demonstration" }
