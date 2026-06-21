package auth

import "context"

// contextWithUser stores the authenticated user id in the context.
func contextWithUser(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserID extracts the authenticated user id from the context (0 if absent).
func UserID(ctx context.Context) int64 {
	if v, ok := ctx.Value(userIDKey).(int64); ok {
		return v
	}
	return 0
}
