package db

import (
	"testing"

	"gorm.io/driver/postgres"

	"github.com/good-fish-man/agent-runtime-client/config"
)

func TestPostgresDialectorUsesSimpleProtocol(t *testing.T) {
	dialector, err := dialector(config.DBConfig{
		DBType:   "postgres",
		DBHost:   "localhost",
		DBPort:   5432,
		Username: "postgres",
		Password: "secret",
		DBName:   "athena",
	})
	if err != nil {
		t.Fatalf("dialector: %v", err)
	}
	postgresDialector, ok := dialector.(*postgres.Dialector)
	if !ok {
		t.Fatalf("dialector type = %T, want *postgres.Dialector", dialector)
	}
	if !postgresDialector.Config.PreferSimpleProtocol {
		t.Fatal("PostgreSQL simple protocol must be enabled to avoid stale cached plans after migration")
	}
}
