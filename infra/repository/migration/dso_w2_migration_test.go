package migration

import (
	"context"
	"reflect"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
)

func TestInitTablesCreatesDSOW2ExecutionArtifacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	initTables := reflect.ValueOf(InitTables)
	args := []reflect.Value{reflect.ValueOf(context.Background()), reflect.ValueOf(data.New(db))}
	if initTables.Type().NumIn() == 3 {
		args = append(args, reflect.Zero(initTables.Type().In(2)))
	}
	results := initTables.Call(args)
	if len(results) != 1 {
		t.Fatalf("InitTables returned %d values, want 1", len(results))
	}
	if err, _ := results[0].Interface().(error); err != nil {
		t.Fatal(err)
	}
	for name, model := range map[string]any{
		"context slice": &po.ContextSlice{}, "capability view": &po.CapabilityView{},
		"actor binding": &po.ActorBinding{}, "verification result": &po.VerificationResult{},
	} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("W2 migration did not create %s", name)
		}
	}
}
