// Package data wraps the gorm handle behind a tiny, context-aware accessor so
// repositories depend on a stable seam (Data.DB(ctx)) rather than on gorm
// directly. This mirrors agent-frame's igo-pkg data.Data without its extras.
package data

import (
	"context"

	"gorm.io/gorm"
)

// Data holds the shared gorm handle.
type Data struct {
	db *gorm.DB
}

// New wraps a gorm handle.
func New(db *gorm.DB) *Data {
	return &Data{db: db}
}

// DB returns a request-scoped gorm session bound to ctx.
func (d *Data) DB(ctx context.Context) *gorm.DB {
	return d.db.WithContext(ctx)
}
