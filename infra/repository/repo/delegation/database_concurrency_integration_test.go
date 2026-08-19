package delegation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
)

func TestDatabaseConcurrencyGate(t *testing.T) {
	tests := []struct {
		name   string
		env    string
		openDB func(string) (gorm.Dialector, error)
	}{
		{name: "postgres", env: "ATHENA_DSO_TEST_POSTGRES_DSN", openDB: func(dsn string) (gorm.Dialector, error) { return postgres.Open(dsn), nil }},
		{name: "mysql", env: "ATHENA_DSO_TEST_MYSQL_DSN", openDB: func(dsn string) (gorm.Dialector, error) { return mysql.Open(dsn), nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := os.Getenv(test.env)
			if dsn == "" {
				t.Skipf("%s is not configured", test.env)
			}
			dialector, err := test.openDB(dsn)
			if err != nil {
				t.Fatal(err)
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			sqlDB.SetMaxOpenConns(40)
			t.Cleanup(func() { _ = sqlDB.Close() })
			resetDelegationTables(t, db)
			t.Cleanup(func() { resetDelegationTables(t, db) })
			store := NewStore(data.New(db))
			assertSingleAttemptOwner(t, store, test.name)
			assertBudgetConservation(t, store, db, test.name)
		})
	}
}

func assertSingleAttemptOwner(t *testing.T, store *Store, suffix string) {
	t.Helper()
	ctx := context.Background()
	runID := "run-owner-" + suffix
	createAcceptedWithManifest(t, store, runID)
	var successful atomic.Int64
	var wg sync.WaitGroup
	for index := 0; index < 32; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			attempt := testAttempt(fmt.Sprintf("attempt-%s-%d", suffix, index), runID, index+1, time.Now().UTC().Add(time.Minute))
			err := store.AcquireAttempt(ctx, attempt, 1, event(fmt.Sprintf("attempt-event-%s-%d", suffix, index), runID, int64(index+2)))
			if err == nil {
				successful.Add(1)
				return
			}
			if !errors.Is(err, ErrAttemptOwned) && !errors.Is(err, ErrRevisionConflict) {
				t.Errorf("unexpected attempt acquisition error: %v", err)
			}
		}(index)
	}
	wg.Wait()
	if successful.Load() != 1 {
		t.Fatalf("database allowed %d active attempt owners", successful.Load())
	}
}

func assertBudgetConservation(t *testing.T, store *Store, db *gorm.DB, suffix string) {
	t.Helper()
	ctx := context.Background()
	budgetRef := "budget-" + suffix
	account := entity.BudgetAccount{
		BudgetRef: budgetRef, OwnerID: "owner-1", Total: entity.BudgetAmount{Tokens: 100, Actions: 10},
		Revision: 1, CreatedAt: testNow, UpdatedAt: testNow,
	}
	if err := store.CreateBudgetAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	var successful atomic.Int64
	var wg sync.WaitGroup
	for index := 0; index < 100; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			reservation := entity.BudgetReservation{
				ReservationID: fmt.Sprintf("reservation-%s-%d", suffix, index), OwnerID: "owner-1", BudgetRef: budgetRef,
				RunID: fmt.Sprintf("budget-run-%s-%d", suffix, index), Requested: entity.BudgetAmount{Tokens: 10, Actions: 1},
				Status: entity.BudgetRequested, Revision: 1, ExpiresAt: testNow.Add(time.Hour), CreatedAt: testNow, UpdatedAt: testNow,
			}
			err := store.ReserveBudget(ctx, reservation, event(fmt.Sprintf("budget-event-%s-%d", suffix, index), reservation.RunID, 1))
			if err == nil {
				successful.Add(1)
				return
			}
			if !errors.Is(err, ErrBudgetExceeded) && !errors.Is(err, ErrRevisionConflict) {
				t.Errorf("unexpected budget reservation error: %v", err)
			}
		}(index)
	}
	wg.Wait()
	stored := readBudgetAccount(t, db, budgetRef)
	if successful.Load() > 10 || stored.Reserved.Tokens > 100 || stored.Reserved.Actions > 10 || !stored.Consumed.Add(stored.Reserved).FitsWithin(stored.Total) {
		t.Fatalf("budget overspent: successful=%d account=%+v", successful.Load(), stored)
	}
	if stored.Reserved.Tokens != successful.Load()*10 || stored.Reserved.Actions != successful.Load() {
		t.Fatalf("budget ledger and committed reservations diverged: successful=%d account=%+v", successful.Load(), stored)
	}
}

func resetDelegationTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	tables := []any{
		&po.Event{}, &po.VerificationResult{}, &po.CandidateResult{}, &po.ResourceLease{}, &po.BudgetReservation{}, &po.BudgetAccount{},
		&po.ModelInvocation{}, &po.DecisionTurn{}, &po.Attempt{}, &po.Run{}, &po.InvocationManifest{},
		&po.ActorBinding{}, &po.CapabilityView{}, &po.ContextSlice{},
		&po.SubagentSpec{}, &po.DelegatedOutcome{}, &po.Decision{}, &po.Proposal{},
	}
	for _, table := range tables {
		_ = db.Migrator().DropTable(table)
	}
	if err := db.AutoMigrate(
		&po.Proposal{}, &po.Decision{}, &po.DelegatedOutcome{}, &po.SubagentSpec{}, &po.ContextSlice{}, &po.CapabilityView{}, &po.ActorBinding{}, &po.InvocationManifest{},
		&po.Run{}, &po.Attempt{}, &po.DecisionTurn{}, &po.ModelInvocation{}, &po.BudgetAccount{},
		&po.BudgetReservation{}, &po.ResourceLease{}, &po.CandidateResult{}, &po.VerificationResult{}, &po.Event{},
	); err != nil {
		t.Fatal(err)
	}
}
