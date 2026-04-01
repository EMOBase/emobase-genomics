package auth

import "context"

// contextKey is unexported to prevent collisions with other packages.
type contextKey string

const usernameKey contextKey = "username"

func WithUsername(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, usernameKey, username)
}

func UsernameFromContext(ctx context.Context) string {
	username, _ := ctx.Value(usernameKey).(string)
	return username
}
