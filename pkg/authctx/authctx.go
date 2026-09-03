package authctx

import "context"

type userIDKey struct{}
type organizationIDKey struct{}

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

func WithOrganizationID(ctx context.Context, organizationID string) context.Context {
	return context.WithValue(ctx, organizationIDKey{}, organizationID)
}

func OrganizationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(organizationIDKey{}).(string)
	return value
}
