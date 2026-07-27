package authctx

import "context"

type userIDKey struct{}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

func UserID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(userIDKey{}).(string)
	return value
}
