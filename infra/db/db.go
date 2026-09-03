// Package db constructs the shared gorm database handle from configuration.
// It supports the same backends agent-frame can target (postgres, mysql) so the
// two services can operate on one database instance.
package db

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/good-fish-man/agent-runtime-client/config"
	log "github.com/good-fish-man/logx"
)

// New opens a *gorm.DB for the given configuration and applies connection-pool
// settings. It returns an error if the backend is unknown or the connection fails.
func New(cfg config.DBConfig) (*gorm.DB, error) {
	dialector, err := dialector(cfg)
	if err != nil {
		return nil, err
	}

	databaseLogLevel := logLevel(cfg.LogMode)
	gdb, err := gorm.Open(dialector, &gorm.Config{
		Logger: newContextualLogger(&log.GormLogger{
			LogLevel:             logLevel(cfg.LogMode),
			SlowThreshold:        time.Duration(cfg.SlowThreshold) * time.Millisecond,
			AdditionalCallerSkip: 1,
		}, databaseLogLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", cfg.DBType, err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("db: handle: %w", err)
	}
	if cfg.MaxOpenConn > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConn)
	}
	if cfg.MaxIdleConn > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConn)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
	}

	return gdb, nil
}

func dialector(cfg config.DBConfig) (gorm.Dialector, error) {
	switch cfg.DBType {
	case "", "postgres", "postgresql":
		// AutoMigrate can change the result shape of SELECT * statements while the
		// service is running. pgx's implicit statement cache may then reuse a stale
		// plan and PostgreSQL rejects it with SQLSTATE 0A000. Simple protocol keeps
		// long-lived connections safe across startup schema migrations.
		return postgres.New(postgres.Config{
			DSN:                  postgresDSN(cfg),
			PreferSimpleProtocol: true,
		}), nil
	case "mysql":
		return mysql.Open(mysqlDSN(cfg)), nil
	default:
		return nil, fmt.Errorf("db: unsupported db_type %q", cfg.DBType)
	}
}

func postgresDSN(cfg config.DBConfig) string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		cfg.DBHost, cfg.DBPort, cfg.Username, cfg.Password, cfg.DBName,
	)
}

func mysqlDSN(cfg config.DBConfig) string {
	charset := cfg.Charset
	if charset == "" {
		charset = "utf8mb4"
	}
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		cfg.Username, cfg.Password, cfg.DBHost, cfg.DBPort, cfg.DBName, charset,
	)
}

// logLevel maps agent-frame's numeric log_mode to gorm's logger level.
func logLevel(mode int) gormlogger.LogLevel {
	switch {
	case mode <= 1:
		return gormlogger.Silent
	case mode == 2:
		return gormlogger.Error
	case mode == 3:
		return gormlogger.Warn
	default:
		return gormlogger.Info
	}
}
