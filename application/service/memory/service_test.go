package memory

import (
	"context"
	"testing"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/memory"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportAndDeleteAllAreOwnerScoped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&po.AgentMemory{}); err != nil {
		t.Fatal(err)
	}

	service := NewService(data.New(db))
	ctx := context.Background()
	for _, fixture := range []struct {
		owner   string
		name    string
		content string
	}{
		{owner: "user-1", name: "first", content: "private one"},
		{owner: "user-1", name: "second", content: "private two"},
		{owner: "user-2", name: "other", content: "must remain"},
	} {
		if _, err := service.Create(ctx, fixture.owner, CreateReq{Name: fixture.name, Description: "secret description", Content: fixture.content}); err != nil {
			t.Fatal(err)
		}
	}

	bundle, err := service.Export(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Schema != "athena.privacy-export.v1" || bundle.OwnerID != "user-1" || len(bundle.Items) != 2 {
		t.Fatalf("unexpected owner export: %#v", bundle)
	}
	for _, item := range bundle.Items {
		if item.UserID != "user-1" {
			t.Fatalf("cross-owner memory leaked into export: %#v", item)
		}
	}

	deleted, err := service.DeleteAll(ctx, "user-1")
	if err != nil || deleted != 2 {
		t.Fatalf("delete all: deleted=%d err=%v", deleted, err)
	}
	items, err := service.List(ctx, "user-1", ListReq{})
	if err != nil || len(items) != 0 {
		t.Fatalf("deleted memories remained active: items=%#v err=%v", items, err)
	}
	other, err := service.List(ctx, "user-2", ListReq{})
	if err != nil || len(other) != 1 || other[0].Content != "must remain" {
		t.Fatalf("other owner's memory changed: items=%#v err=%v", other, err)
	}

	var tombstones []po.AgentMemory
	if err := db.Where("user_id = ?", "user-1").Find(&tombstones).Error; err != nil {
		t.Fatal(err)
	}
	if len(tombstones) != 2 {
		t.Fatalf("expected two audit tombstones, got %d", len(tombstones))
	}
	for _, item := range tombstones {
		if item.DeletedAt == 0 || item.Enabled || item.Content != "" || item.Name != "" || item.Description != "" {
			t.Fatalf("deleted memory retained private payload: %#v", item)
		}
	}
}
