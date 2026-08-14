package migration

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	chatpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/chat"
	memorypo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/memory"
	operationspo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/operations"
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
	if err := InitTables(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	if err := InitTables(context.Background(), store); err != nil {
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
}
