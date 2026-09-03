package learning

import (
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/learning"
)

func TestMaterializeRejectsCrossOwnerArtifactIDCollision(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&po.Skill{}, &po.SkillVersion{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	first := entity.Candidate{
		CandidateID: "candidate-1", OwnerID: "owner-1", UpdatedAt: now,
		Skill: &entity.SkillDefinition{
			ID: "shared.logical.id", Version: "1.0.0", OwnerID: "owner-1",
			Visibility: entity.VisibilityPrivate, LifecycleState: entity.LifecycleApproved,
		},
	}
	if err := materialize(db, first, "reviewer-1", ""); err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	second := entity.Candidate{
		CandidateID: "candidate-2", OwnerID: "owner-2", UpdatedAt: now.Add(time.Second),
		Skill: &entity.SkillDefinition{
			ID: "shared.logical.id", Version: "2.0.0", OwnerID: "owner-2",
			Visibility: entity.VisibilityPrivate, LifecycleState: entity.LifecycleApproved,
		},
	}
	err = db.Transaction(func(tx *gorm.DB) error { return materialize(tx, second, "reviewer-2", "") })
	if err == nil || !strings.Contains(err.Error(), "already owned by another user") {
		t.Fatalf("cross-owner materialize error = %v", err)
	}
	var stored po.Skill
	if err := db.Where("skill_id = ?", first.Skill.ID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.OwnerID != first.OwnerID || stored.LatestVersion != first.Skill.Version {
		t.Fatalf("first owner's artifact was changed: %#v", stored)
	}
	var versions int64
	if err := db.Model(&po.SkillVersion{}).Where("skill_id = ?", first.Skill.ID).Count(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Fatalf("version count = %d, want 1", versions)
	}
}
