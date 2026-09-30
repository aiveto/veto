package auth

import "context"

type userTokenKey struct{}

// WithUserToken attaches the token the MCP host passed on this invoke.
// Login and client credentials ignore it. An invoke source sends it.
// A command receives it only when it is set.
func WithUserToken(ctx context.Context, token string) context.Context {
	if ctx == nil || token == "" {
		return ctx
	}
	return context.WithValue(ctx, userTokenKey{}, token)
}

func UserToken(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(userTokenKey{}).(string)
	return v
}
