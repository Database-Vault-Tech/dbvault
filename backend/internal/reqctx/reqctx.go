// Package reqctx carries per-request identity and metadata through context.
package reqctx

import "context"

type principalKey struct{}
type membershipKey struct{}
type metaKey struct{}

// Principal is the authenticated caller.
type Principal struct {
	UserID    string
	Email     string
	Name      string
	SessionID string // set for browser sessions
	TokenID   string // set for API tokens (CLI)
}

// ActorType is how the audit log labels this caller.
func (p Principal) ActorType() string {
	if p.TokenID != "" {
		return "api_token"
	}
	return "user"
}

// Membership is the caller's role in the organization the request targets.
type Membership struct {
	OrgID   string
	OrgName string
	OrgSlug string
	Role    string
}

// Meta holds request metadata used for logging and auditing.
type Meta struct {
	RequestID string
	IP        string
	UserAgent string
}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

func WithMembership(ctx context.Context, m Membership) context.Context {
	return context.WithValue(ctx, membershipKey{}, m)
}

func MembershipFrom(ctx context.Context) (Membership, bool) {
	m, ok := ctx.Value(membershipKey{}).(Membership)
	return m, ok
}

func WithMeta(ctx context.Context, m Meta) context.Context {
	return context.WithValue(ctx, metaKey{}, m)
}

func MetaFrom(ctx context.Context) Meta {
	m, _ := ctx.Value(metaKey{}).(Meta)
	return m
}
