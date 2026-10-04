package auth

import "context"

type (
	userTokenKey struct{}
	callerKey    struct{}
)

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

// WithCaller records who is calling veto. The value is an identity, not a credential.
func WithCaller(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, callerKey{}, id)
}

func Caller(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(callerKey{}).(string)
	return v
}

// OrLocal is the caller name on the wire. Empty is local.
func OrLocal(id string) string {
	if id == "" {
		return "local"
	}
	return id
}

type forceKey struct{}

// WithForce tells a provider to skip a cached credential and obtain a new one.
func WithForce(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, forceKey{}, true)
}

func forced(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(forceKey{}).(bool)
	return v
}
