package db

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/good-fish-man/agent-runtime-client/pkg/dbctx"
	log "github.com/good-fish-man/logx"
)

type recordedDatabaseLog struct {
	kind  string
	level logger.LogLevel
}

type recordingGormLogger struct {
	level logger.LogLevel
	calls *[]recordedDatabaseLog
}

func (l *recordingGormLogger) LogMode(level logger.LogLevel) logger.Interface {
	return &recordingGormLogger{level: level, calls: l.calls}
}

func (l *recordingGormLogger) Info(context.Context, string, ...any) {
	if l.level >= logger.Info {
		*l.calls = append(*l.calls, recordedDatabaseLog{kind: "info", level: l.level})
	}
}

func (l *recordingGormLogger) Warn(context.Context, string, ...any) {
	if l.level >= logger.Warn {
		*l.calls = append(*l.calls, recordedDatabaseLog{kind: "warn", level: l.level})
	}
}

func (l *recordingGormLogger) Error(context.Context, string, ...any) {
	if l.level >= logger.Error {
		*l.calls = append(*l.calls, recordedDatabaseLog{kind: "error", level: l.level})
	}
}

func (l *recordingGormLogger) Trace(_ context.Context, _ time.Time, _ func() (string, int64), err error) {
	if err != nil && l.level >= logger.Error {
		*l.calls = append(*l.calls, recordedDatabaseLog{kind: "trace-error", level: l.level})
		return
	}
	if l.level == logger.Info {
		*l.calls = append(*l.calls, recordedDatabaseLog{kind: "trace-info", level: l.level})
	}
}

func TestContextualLoggerSuppressesOnlyPollingInfo(t *testing.T) {
	calls := make([]recordedDatabaseLog, 0)
	value := newContextualLogger(&recordingGormLogger{calls: &calls}, logger.Info)
	query := func() (string, int64) { return "SELECT 1", 1 }

	value.Trace(context.Background(), time.Now(), query, nil)
	quiet := dbctx.SuppressQueryInfo(context.Background())
	value.Trace(quiet, time.Now(), query, nil)
	value.Info(quiet, "query")
	value.Trace(quiet, time.Now(), query, context.DeadlineExceeded)

	if len(calls) != 2 || calls[0].kind != "trace-info" || calls[0].level != logger.Info {
		t.Fatalf("ordinary database logs = %+v", calls)
	}
	if calls[1].kind != "trace-error" || calls[1].level != logger.Warn {
		t.Fatalf("polling database error was not retained at Warn: %+v", calls)
	}
}

func TestContextualLoggerDoesNotRaiseConfiguredLevel(t *testing.T) {
	for _, level := range []logger.LogLevel{logger.Silent, logger.Error, logger.Warn} {
		calls := make([]recordedDatabaseLog, 0)
		value := newContextualLogger(&recordingGormLogger{calls: &calls}, level)
		value.Trace(dbctx.SuppressQueryInfo(context.Background()), time.Now(), func() (string, int64) {
			return "SELECT 1", 1
		}, nil)
		if len(calls) != 0 {
			t.Fatalf("configured level %d was raised for polling: %+v", level, calls)
		}
	}
}

func TestContextualLoggerAttributesSQLToQueryCaller(t *testing.T) {
	var output bytes.Buffer
	previousLevel := log.GetLogLevel()
	log.SetOutput(&output)
	log.SetLogLevel(log.DebugLevel)
	defer func() {
		log.SetOutput(nil)
		log.SetLogLevel(previousLevel)
	}()

	type callerProbe struct{ ID int }
	database, err := gorm.Open(sqlite.Open("file:db-logger-caller?mode=memory&cache=shared"), &gorm.Config{
		Logger: newContextualLogger(&log.GormLogger{
			LogLevel:             logger.Info,
			SlowThreshold:        time.Second,
			AdditionalCallerSkip: 1,
		}, logger.Info),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&callerProbe{}); err != nil {
		t.Fatal(err)
	}
	output.Reset()

	var rows []callerProbe
	if err := database.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "infra/db/logger_test.go:") {
		t.Fatalf("SQL caller was not attributed to the query call site: %s", got)
	}
	if strings.Contains(got, "gorm.io/gorm/finisher_api.go:") {
		t.Fatalf("SQL caller was attributed to GORM internals: %s", got)
	}
}
