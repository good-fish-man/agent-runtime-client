package db

import (
	"context"
	"time"

	"gorm.io/gorm/logger"

	"github.com/good-fish-man/agent-runtime-client/pkg/dbctx"
)

// contextualLogger keeps the configured database log level for normal work,
// while high-frequency polling contexts omit successful Info-level SQL. Slow
// queries and errors continue through the Warn-level logger.
type contextualLogger struct {
	standard logger.Interface
	polling  logger.Interface
}

func newContextualLogger(base logger.Interface, level logger.LogLevel) logger.Interface {
	pollingLevel := level
	if pollingLevel > logger.Warn {
		pollingLevel = logger.Warn
	}
	return &contextualLogger{
		standard: base.LogMode(level),
		polling:  base.LogMode(pollingLevel),
	}
}

func (l *contextualLogger) LogMode(level logger.LogLevel) logger.Interface {
	return newContextualLogger(l.standard, level)
}

func (l *contextualLogger) Info(ctx context.Context, message string, values ...any) {
	l.forContext(ctx).Info(ctx, message, values...)
}

func (l *contextualLogger) Warn(ctx context.Context, message string, values ...any) {
	l.forContext(ctx).Warn(ctx, message, values...)
}

func (l *contextualLogger) Error(ctx context.Context, message string, values ...any) {
	l.forContext(ctx).Error(ctx, message, values...)
}

func (l *contextualLogger) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	l.forContext(ctx).Trace(ctx, begin, query, err)
}

func (l *contextualLogger) forContext(ctx context.Context) logger.Interface {
	if dbctx.QueryInfoSuppressed(ctx) {
		return l.polling
	}
	return l.standard
}
