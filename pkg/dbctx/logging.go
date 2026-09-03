// Package dbctx carries database logging policy through request contexts.
package dbctx

import "context"

type suppressQueryInfoKey struct{}

// SuppressQueryInfo marks high-frequency polling work whose successful SQL
// statements should not be logged at Info. Database warnings and errors remain
// visible.
func SuppressQueryInfo(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, suppressQueryInfoKey{}, true)
}

// QueryInfoSuppressed reports whether successful SQL Info logs should be
// omitted for work executed with ctx.
func QueryInfoSuppressed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(suppressQueryInfoKey{}).(bool)
	return value
}
